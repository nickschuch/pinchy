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

	"github.com/nickschuch/pinchy/internal/config"
	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

// Default image references. Override per-invocation with --agent-image,
// --docker-image, --proxy-image, --console-image, or --llmproxy-image, or via
// the corresponding env vars.
const (
	DefaultAgentImage    = "ghcr.io/nickschuch/pinchy-agent:latest"
	DefaultDockerImage   = "ghcr.io/nickschuch/pinchy-docker:latest"
	DefaultProxyImage    = "ghcr.io/nickschuch/pinchy-proxy:latest"
	DefaultConsoleImage  = "ghcr.io/nickschuch/pinchy-console:latest"
	DefaultLLMProxyImage = "ghcr.io/nickschuch/pinchy-llmproxy:latest"
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

// ensureSharedInfra ensures the shared bridge network, Traefik proxy,
// discovery console, and (when configured) the LLM proxy exist and are
// running. It is called from create, start, restart, and init so callers do
// not need to run `pinchy init` separately.
//
// consoleImage may be empty; when it is, the console container is skipped.
//
// llmproxyCfg may be nil; when it is, or when AnthropicAPIKey is empty, the
// LLM proxy is not started. When it is non-nil and has a key, the LLM proxy
// is started and its failure is fatal (not best-effort), because agents are
// hard-wired to use it.
//
// When recreate is true the proxy, console, and llmproxy containers are
// removed before the normal ensure flow. The shared network is never removed.
func ensureSharedInfra(ctx context.Context, cli *client.Client, proxyImage, consoleImage, llmproxyImage, version string, healthTimeout time.Duration, recreate bool, llmproxyCfg *config.LLMProxyConfig) error {
	if recreate {
		if err := dockerx.RemoveContainer(ctx, cli, pinchyenv.ProxyContainerName, true); err != nil {
			return fmt.Errorf("removing proxy container: %w", err)
		}
		if err := dockerx.RemoveContainer(ctx, cli, pinchyenv.ConsoleContainerName, true); err != nil {
			return fmt.Errorf("removing console container: %w", err)
		}
		if err := dockerx.RemoveContainer(ctx, cli, pinchyenv.LLMProxyContainerName, true); err != nil {
			return fmt.Errorf("removing llmproxy container: %w", err)
		}
	}

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

	// Console is best-effort: if the image is unavailable or the container
	// fails to start, print a warning but do not block the rest of init.
	if consoleImage != "" {
		if err := dockerx.EnsureImageLocal(ctx, cli, consoleImage); err == nil {
			if _, err := dockerx.EnsureConsole(ctx, cli, dockerx.ConsoleConfig{
				Image:   consoleImage,
				Version: version,
			}); err != nil {
				fmt.Printf("warning: could not start console container: %v\n", err)
			}
		}
	}

	// LLM proxy is FATAL when configured: agents are hard-wired to use it, so
	// a configured-but-broken proxy would cause silent failures for every agent.
	if llmproxyCfg != nil && llmproxyCfg.AnthropicAPIKey != "" {
		if err := dockerx.EnsureImageLocal(ctx, cli, llmproxyImage); err != nil {
			return fmt.Errorf("pulling llmproxy image: %w", err)
		}
		if _, err := dockerx.EnsureLLMProxy(ctx, cli, dockerx.LLMProxyConfig{
			Image:           llmproxyImage,
			Version:         version,
			AnthropicAPIKey: llmproxyCfg.AnthropicAPIKey,
		}); err != nil {
			return fmt.Errorf("ensuring llmproxy: %w", err)
		}
		llmWaitCtx, llmCancel := waitContext(ctx, healthTimeout)
		defer llmCancel()
		if err := dockerx.WaitForLLMProxyHealthy(llmWaitCtx, cli, 3*time.Second); err != nil {
			return fmt.Errorf("llmproxy never became healthy: %w", err)
		}
	}

	return nil
}
