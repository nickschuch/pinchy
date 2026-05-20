package dockerx

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"

	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

// ConsoleConfig holds parameters for the console container.
type ConsoleConfig struct {
	Image   string
	Version string
}

// consoleContainerConfig builds the container.Config and container.HostConfig
// for the discovery console.
func consoleContainerConfig(cfg ConsoleConfig) (*container.Config, *container.HostConfig) {
	created := time.Now().UTC().Format(time.RFC3339)
	labels := pinchyenv.ServiceLabels(pinchyenv.RoleConsole, cfg.Version, created)
	for k, v := range pinchyenv.ConsoleTraefikLabels() {
		labels[k] = v
	}

	containerCfg := &container.Config{
		Image:  cfg.Image,
		Labels: labels,
		// Cmd is intentionally omitted — the Dockerfile ENTRYPOINT
		// ["pinchy", "console", "serve"] is the sole source of truth for
		// how the container starts. Setting Cmd here would cause it to be
		// appended to the ENTRYPOINT, producing a doubled command path.
		Healthcheck: &container.HealthConfig{
			Test:        []string{"CMD", "wget", "-qO-", "http://localhost:8080/healthz"},
			Interval:    5 * time.Second,
			Timeout:     3 * time.Second,
			Retries:     12,
			StartPeriod: 5 * time.Second,
		},
	}

	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeBind,
				Source:   "/var/run/docker.sock",
				Target:   "/var/run/docker.sock",
				ReadOnly: true,
			},
		},
		NetworkMode: container.NetworkMode(pinchyenv.SharedNetworkName),
	}

	return containerCfg, hostCfg
}

// EnsureConsole creates and starts the console container if it does not already
// exist. If it exists but is stopped, it is started. If the image or command
// has changed, it is removed and recreated. Returns the container ID.
func EnsureConsole(ctx context.Context, cli *client.Client, cfg ConsoleConfig) (string, error) {
	insp, err := cli.ContainerInspect(ctx, pinchyenv.ConsoleContainerName)
	if err != nil && !errdefs.IsNotFound(err) {
		return "", fmt.Errorf("inspecting console container: %w", err)
	}

	if err == nil {
		desiredCfg, _ := consoleContainerConfig(cfg)
		imageChanged := insp.Config.Image != cfg.Image
		cmdChanged := !reflect.DeepEqual(insp.Config.Cmd, desiredCfg.Cmd)
		healthcheckChanged := !reflect.DeepEqual(insp.Config.Healthcheck, desiredCfg.Healthcheck)

		if imageChanged || cmdChanged || healthcheckChanged {
			if err := RemoveContainer(ctx, cli, pinchyenv.ConsoleContainerName, true); err != nil {
				return "", fmt.Errorf("removing stale console container: %w", err)
			}
		} else {
			if !insp.State.Running {
				if err := StartContainer(ctx, cli, pinchyenv.ConsoleContainerName); err != nil {
					return "", err
				}
			}
			return insp.ID, nil
		}
	}

	containerCfg, hostCfg := consoleContainerConfig(cfg)
	id, err := CreateAndStart(ctx, cli, pinchyenv.ConsoleContainerName, containerCfg, hostCfg, pinchyenv.SharedNetworkName)
	if err != nil {
		return "", fmt.Errorf("creating console container: %w", err)
	}
	return id, nil
}

// WaitForConsoleHealthy polls the console container until it is healthy or the
// context deadline is exceeded.
func WaitForConsoleHealthy(ctx context.Context, cli *client.Client, pollInterval time.Duration) error {
	return WaitForHealthy(ctx, cli, pinchyenv.ConsoleContainerName, pollInterval)
}

// ConsoleStatus inspects the console container and returns an env.Service
// describing its current state. The second return value is false when no
// console container exists.
func ConsoleStatus(ctx context.Context, cli *client.Client) (pinchyenv.Service, bool, error) {
	insp, err := cli.ContainerInspect(ctx, pinchyenv.ConsoleContainerName)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return pinchyenv.Service{}, false, nil
		}
		return pinchyenv.Service{}, false, fmt.Errorf("inspecting console container: %w", err)
	}

	svc := pinchyenv.Service{
		Name:  "console",
		Ports: []int{8080},
	}

	if insp.State != nil {
		svc.Status = insp.State.Status
		if insp.State.Health != nil {
			svc.Health = insp.State.Health.Status
		}
	}

	if t, err := time.Parse(time.RFC3339, insp.Config.Labels[pinchyenv.LabelCreated]); err == nil {
		svc.Created = t
	}

	return svc, true, nil
}

// ConsoleLogs streams the console container's logs to w. If follow is true,
// the stream stays open until the context is cancelled.
func ConsoleLogs(ctx context.Context, cli *client.Client, follow bool, w io.Writer) error {
	rc, err := cli.ContainerLogs(ctx, pinchyenv.ConsoleContainerName, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: true,
	})
	if err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("console container %q not found — run `pinchy init` first", pinchyenv.ConsoleContainerName)
		}
		return fmt.Errorf("fetching console logs: %w", err)
	}
	defer rc.Close()

	_, err = io.Copy(w, rc)
	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		return fmt.Errorf("reading console logs: %w", err)
	}
	return nil
}

// AgentEnvVars inspects the given agent container and returns a map of the
// environment variables set on it (e.g. OPENCODE_SERVER_PASSWORD). This is
// used by the console to authenticate against per-environment opencode servers.
func AgentEnvVars(ctx context.Context, cli *client.Client, containerName string) (map[string]string, error) {
	insp, err := cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(insp.Config.Env))
	for _, kv := range insp.Config.Env {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		} else {
			result[parts[0]] = ""
		}
	}
	return result, nil
}
