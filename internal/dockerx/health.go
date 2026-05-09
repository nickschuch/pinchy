package dockerx

import (
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
