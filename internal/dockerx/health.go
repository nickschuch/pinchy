package dockerx

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// WaitForHealthy polls the named container until its healthcheck reports
// "healthy" or the context is cancelled. It returns an error if the
// container exits, is reported "unhealthy", or has no healthcheck configured.
func WaitForHealthy(ctx context.Context, cli *client.Client, name string, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 1 * time.Second
	}
	for {
		insp, err := cli.ContainerInspect(ctx, name)
		if err != nil {
			return fmt.Errorf("inspecting container %q: %w", name, err)
		}
		if insp.State == nil {
			return fmt.Errorf("container %q has no state", name)
		}
		if !insp.State.Running {
			return fmt.Errorf("container %q exited before becoming healthy: status=%s exitCode=%d", name, insp.State.Status, insp.State.ExitCode)
		}
		if insp.State.Health == nil {
			return fmt.Errorf("container %q has no healthcheck configured", name)
		}
		switch insp.State.Health.Status {
		case container.Healthy:
			return nil
		case container.Unhealthy:
			return fmt.Errorf("container %q is unhealthy", name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// WaitForAgentReady waits until the opencode web server inside the named agent
// container is ready to accept connections on http://localhost:4096/global/health.
//
// Strategy:
//  1. If the container has a Docker HEALTHCHECK configured (new image), delegate
//     to WaitForHealthy so we get Traefik-consistent behaviour.
//  2. If the container has no HEALTHCHECK (old image, pre-HEALTHCHECK), fall back
//     to polling the endpoint directly via `docker exec curl` so that environments
//     created before the image rebuild are not permanently broken.
//
// The fallback emits a one-time warning line via warnf so callers can surface it
// to the user.
func WaitForAgentReady(ctx context.Context, cli *client.Client, name string, pollInterval time.Duration, warnf func(string, ...any)) error {
	if pollInterval <= 0 {
		pollInterval = 1 * time.Second
	}

	// Peek at the container to decide which strategy to use.
	insp, err := cli.ContainerInspect(ctx, name)
	if err != nil {
		return fmt.Errorf("inspecting container %q: %w", name, err)
	}
	if insp.State == nil {
		return fmt.Errorf("container %q has no state", name)
	}

	hasHealthcheck := insp.Config != nil && insp.Config.Healthcheck != nil &&
		len(insp.Config.Healthcheck.Test) > 0 && insp.Config.Healthcheck.Test[0] != "NONE"

	if hasHealthcheck {
		return WaitForHealthy(ctx, cli, name, pollInterval)
	}

	// Fallback: HTTP poll via docker exec.
	if warnf != nil {
		warnf("note: agent image has no HEALTHCHECK; polling /global/health via exec (rebuild image to enable native healthcheck)\n")
	}
	return waitForAgentReadyExec(ctx, cli, name, pollInterval)
}

// waitForAgentReadyExec polls http://localhost:4096/global/health inside the
// named container by running `curl -fsS <url>` via docker exec until it
// succeeds or the context is cancelled.
func waitForAgentReadyExec(ctx context.Context, cli *client.Client, name string, pollInterval time.Duration) error {
	for {
		// Check container is still running before each attempt.
		insp, err := cli.ContainerInspect(ctx, name)
		if err != nil {
			return fmt.Errorf("inspecting container %q: %w", name, err)
		}
		if insp.State == nil || !insp.State.Running {
			return fmt.Errorf("container %q stopped while waiting for agent to become ready", name)
		}

		execID, err := cli.ContainerExecCreate(ctx, name, container.ExecOptions{
			Cmd:          []string{"curl", "-fsS", "http://localhost:4096/global/health"},
			AttachStdout: true,
			AttachStderr: true,
		})
		if err != nil {
			return fmt.Errorf("creating exec in %q: %w", name, err)
		}

		resp, err := cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
		if err != nil {
			return fmt.Errorf("attaching exec in %q: %w", name, err)
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Reader)
		resp.Close()

		ei, err := cli.ContainerExecInspect(ctx, execID.ID)
		if err != nil {
			return fmt.Errorf("inspecting exec in %q: %w", name, err)
		}
		if ei.ExitCode == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
