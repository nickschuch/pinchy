package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/docker/docker/client"
	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
)

// Default image references. Override per-invocation with --agent-image,
// --docker-image, or --proxy-image, or via the corresponding env vars.
const (
	DefaultAgentImage  = "ghcr.io/nickschuch/pinchy-agent:latest"
	DefaultDockerImage = "ghcr.io/nickschuch/pinchy-docker:latest"
	DefaultProxyImage  = "ghcr.io/nickschuch/pinchy-proxy:latest"
)

// resolveImage chooses the effective image reference for one role using the
// documented precedence: flag > env var > default.
func resolveImage(flagValue, envVar, def string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return def
}

// resolveWorkdir converts a possibly-relative --workdir flag value into an
// absolute path. An empty value defaults to the current working directory.
func resolveWorkdir(raw string) (string, error) {
	if raw == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determining current working directory: %w", err)
		}
		return wd, nil
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolving --workdir %q: %w", raw, err)
	}
	return abs, nil
}

// runDockerExecTTY shells out to the docker CLI for an interactive exec
// session. Going through `docker exec -it` rather than the engine's
// Hijack API avoids us having to re-implement TTY/raw-mode handling and
// detach-key processing.
func runDockerExecTTY(ctx context.Context, container string, cmdAndArgs []string) error {
	return runDockerExecTTYEnv(ctx, container, nil, cmdAndArgs)
}

// runDockerExecTTYEnv is like runDockerExecTTY but also injects env into the
// exec session via docker exec -e KEY=VALUE flags.
func runDockerExecTTYEnv(ctx context.Context, container string, env []string, cmdAndArgs []string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker CLI not found in PATH: %w", err)
	}
	args := []string{"exec", "-it"}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, container)
	args = append(args, cmdAndArgs...)
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// runDockerExecPlain shells out to the docker CLI without a TTY (useful for
// non-interactive `pinchy exec`).
func runDockerExecPlain(ctx context.Context, container string, cmdAndArgs []string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker CLI not found in PATH: %w", err)
	}
	args := append([]string{"exec", "-i", container}, cmdAndArgs...)
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// addCommonFlags registers the flags shared by commands that need to refer
// to a specific service within an environment.
func addServiceFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "service", "agent", "service to target: agent or docker")
}

// ensureSharedInfra ensures the shared bridge network and Traefik proxy
// container exist and are running. It is called from create, start, restart,
// and init so callers do not need to run `pinchy init` separately.
func ensureSharedInfra(ctx context.Context, cli *client.Client, proxyImage, version string, healthTimeout time.Duration) error {
	if err := dockerx.EnsureSharedNetwork(ctx, cli, version); err != nil {
		return fmt.Errorf("ensuring shared network: %w", err)
	}
	if err := dockerx.EnsureImageLocal(ctx, cli, proxyImage); err != nil {
		return err
	}
	if _, err := dockerx.EnsureProxy(ctx, cli, dockerx.ProxyConfig{
		Image:   proxyImage,
		Version: version,
	}); err != nil {
		return fmt.Errorf("ensuring proxy: %w", err)
	}
	waitCtx, cancel := waitContext(ctx, healthTimeout)
	defer cancel()
	if err := dockerx.WaitForProxyHealthy(waitCtx, cli, 2*time.Second); err != nil {
		return fmt.Errorf("proxy never became healthy: %w", err)
	}
	return nil
}
