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
	"github.com/docker/go-connections/nat"

	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

// proxyPorts are the host/container ports the Traefik proxy binds.
var proxyPorts = []string{"8080", "4096", "3000"}

// ProxyConfig holds parameters for the shared proxy container.
type ProxyConfig struct {
	Image   string
	Version string
}

// proxyContainerConfig builds the container.Config and container.HostConfig
// for the Traefik proxy.
func proxyContainerConfig(cfg ProxyConfig) (*container.Config, *container.HostConfig) {
	created := time.Now().UTC().Format(time.RFC3339)
	labels := pinchyenv.ServiceLabels(pinchyenv.RoleProxy, cfg.Version, created)

	// Build port bindings: each of proxyPorts maps host:port -> container:port.
	portSet := nat.PortSet{}
	portBindings := nat.PortMap{}
	for _, p := range proxyPorts {
		cp := nat.Port(p + "/tcp")
		portSet[cp] = struct{}{}
		portBindings[cp] = []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: p}}
	}

	containerCfg := &container.Config{
		Image:        cfg.Image,
		ExposedPorts: portSet,
		Labels:       labels,
		Healthcheck: &container.HealthConfig{
			// The healthcheck runs as a separate subprocess with no inherited CLI
			// flags, so we must re-supply the minimum flags that traefik healthcheck
			// needs to locate and probe the ping endpoint.
			Test: []string{
				"CMD", "traefik", "healthcheck",
				"--ping=true",
				"--ping.entryPoint=p8080",
				"--entrypoints.p8080.address=:8080",
			},
			Interval:    5 * time.Second,
			Timeout:     3 * time.Second,
			Retries:     12,
			StartPeriod: 5 * time.Second,
		},
		Cmd: []string{
			"--ping=true",
			"--ping.entryPoint=p8080",
			"--providers.docker=true",
			"--providers.docker.exposedbydefault=false",
			"--providers.docker.network=" + pinchyenv.SharedNetworkName,
			`--providers.docker.constraints=Label(` + "`" + `pinchy.managed` + "`" + `,` + "`" + `true` + "`" + `)`,
			"--entrypoints.p8080.address=:8080",
			"--entrypoints.p4096.address=:4096",
			"--entrypoints.p3000.address=:3000",
		},
	}

	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		PortBindings:  portBindings,
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

// EnsureProxy creates and starts the shared Traefik proxy container if it does
// not already exist. If it exists but is stopped, it is started. If it exists
// with a different image, it is removed and recreated. Returns the container ID.
func EnsureProxy(ctx context.Context, cli *client.Client, cfg ProxyConfig) (string, error) {
	insp, err := cli.ContainerInspect(ctx, pinchyenv.ProxyContainerName)
	if err != nil && !errdefs.IsNotFound(err) {
		return "", fmt.Errorf("inspecting proxy container: %w", err)
	}

	if err == nil {
		// Container exists — check if image or command has changed.
		desiredCfg, _ := proxyContainerConfig(cfg)
		imageChanged := insp.Config.Image != cfg.Image
		cmdChanged := !reflect.DeepEqual(insp.Config.Cmd, desiredCfg.Cmd)
		healthcheckChanged := !reflect.DeepEqual(insp.Config.Healthcheck, desiredCfg.Healthcheck)

		if imageChanged || cmdChanged || healthcheckChanged {
			// Configuration changed: remove and recreate.
			if err := RemoveContainer(ctx, cli, pinchyenv.ProxyContainerName, true); err != nil {
				return "", fmt.Errorf("removing stale proxy container: %w", err)
			}
		} else {
			// Same config — start if not running.
			if !insp.State.Running {
				if err := StartContainer(ctx, cli, pinchyenv.ProxyContainerName); err != nil {
					return "", err
				}
			}
			return insp.ID, nil
		}
	}

	// Create fresh.
	containerCfg, hostCfg := proxyContainerConfig(cfg)
	id, err := CreateAndStart(ctx, cli, pinchyenv.ProxyContainerName, containerCfg, hostCfg, pinchyenv.SharedNetworkName)
	if err != nil {
		return "", fmt.Errorf("creating proxy container: %w", err)
	}
	return id, nil
}

// WaitForProxyHealthy polls the proxy container until it is healthy or the
// context deadline is exceeded.
func WaitForProxyHealthy(ctx context.Context, cli *client.Client, pollInterval time.Duration) error {
	return WaitForHealthy(ctx, cli, pinchyenv.ProxyContainerName, pollInterval)
}

// ProxyStatus inspects the proxy container and returns an env.Service
// describing its current state. The second return value is false when no proxy
// container exists.
func ProxyStatus(ctx context.Context, cli *client.Client) (pinchyenv.Service, bool, error) {
	insp, err := cli.ContainerInspect(ctx, pinchyenv.ProxyContainerName)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return pinchyenv.Service{}, false, nil
		}
		return pinchyenv.Service{}, false, fmt.Errorf("inspecting proxy container: %w", err)
	}

	svc := pinchyenv.Service{
		Name:  "proxy",
		Ports: []int{8080, 4096, 3000},
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

// ProxyLogs streams the proxy container's logs to w. If follow is true, the
// stream stays open until the context is cancelled.
func ProxyLogs(ctx context.Context, cli *client.Client, follow bool, w io.Writer) error {
	rc, err := cli.ContainerLogs(ctx, pinchyenv.ProxyContainerName, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: true,
	})
	if err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("proxy container %q not found — run `pinchy init` first", pinchyenv.ProxyContainerName)
		}
		return fmt.Errorf("fetching proxy logs: %w", err)
	}
	defer rc.Close()

	_, err = io.Copy(w, rc)
	// A cancelled context causes an EOF-like error; treat that as success.
	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		return fmt.Errorf("reading proxy logs: %w", err)
	}
	return nil
}
