package dockerx

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"

	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

// LLMProxyConfig holds parameters for the shared LLM proxy container.
type LLMProxyConfig struct {
	Image           string
	Version         string
	AnthropicAPIKey string
}

// keyHash returns a short (16-char) sha256 hex prefix of the given key. It is
// stored as a label for change detection only — never the full key.
func keyHash(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", h[:8])
}

// llmproxyContainerConfig builds the container.Config and container.HostConfig
// for the LLM proxy container.
func llmproxyContainerConfig(cfg LLMProxyConfig) (*container.Config, *container.HostConfig) {
	created := time.Now().UTC().Format(time.RFC3339)
	labels := pinchyenv.ServiceLabels(pinchyenv.RoleLLMProxy, cfg.Version, created)
	labels[pinchyenv.LabelLLMProxyKeyHash] = keyHash(cfg.AnthropicAPIKey)
	// Merge Traefik labels so the proxy routes /ui traffic to this container.
	for k, v := range pinchyenv.LLMProxyTraefikLabels() {
		labels[k] = v
	}

	containerCfg := &container.Config{
		Image: cfg.Image,
		Env: []string{
			"ANTHROPIC_API_KEY=" + cfg.AnthropicAPIKey,
		},
		Labels: labels,
		Healthcheck: &container.HealthConfig{
			Test: []string{
				"CMD-SHELL",
				"python3 -c \"import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://localhost:" + pinchyenv.LLMProxyPort + "/health/liveliness').status==200 else 1)\"",
			},
			Interval:    10 * time.Second,
			Timeout:     5 * time.Second,
			Retries:     12,
			StartPeriod: 30 * time.Second,
		},
	}

	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		// No port bindings — the proxy is internal only, reachable from
		// containers on pinchy-shared. The admin UI is routed via Traefik.
		NetworkMode: container.NetworkMode(pinchyenv.SharedNetworkName),
	}

	return containerCfg, hostCfg
}

// EnsureLLMProxy creates and starts the shared LLM proxy container if it does
// not already exist. If it exists but is stopped, it is started. If the image,
// healthcheck, or API key has changed it is removed and recreated. Returns the
// container ID.
func EnsureLLMProxy(ctx context.Context, cli *client.Client, cfg LLMProxyConfig) (string, error) {
	insp, err := cli.ContainerInspect(ctx, pinchyenv.LLMProxyContainerName)
	if err != nil && !errdefs.IsNotFound(err) {
		return "", fmt.Errorf("inspecting llmproxy container: %w", err)
	}

	if err == nil {
		// Container exists — check if anything has changed.
		desiredCfg, _ := llmproxyContainerConfig(cfg)
		imageChanged := insp.Config.Image != cfg.Image
		healthcheckChanged := !reflect.DeepEqual(insp.Config.Healthcheck, desiredCfg.Healthcheck)
		keyChanged := insp.Config.Labels[pinchyenv.LabelLLMProxyKeyHash] != keyHash(cfg.AnthropicAPIKey)

		if imageChanged || healthcheckChanged || keyChanged {
			if err := RemoveContainer(ctx, cli, pinchyenv.LLMProxyContainerName, true); err != nil {
				return "", fmt.Errorf("removing stale llmproxy container: %w", err)
			}
		} else {
			if !insp.State.Running {
				if err := StartContainer(ctx, cli, pinchyenv.LLMProxyContainerName); err != nil {
					return "", err
				}
			}
			return insp.ID, nil
		}
	}

	// Create fresh.
	containerCfg, hostCfg := llmproxyContainerConfig(cfg)
	id, err := CreateAndStart(ctx, cli, pinchyenv.LLMProxyContainerName, containerCfg, hostCfg, pinchyenv.SharedNetworkName)
	if err != nil {
		return "", fmt.Errorf("creating llmproxy container: %w", err)
	}
	return id, nil
}

// WaitForLLMProxyHealthy polls the LLM proxy container until it is healthy or
// the context deadline is exceeded.
func WaitForLLMProxyHealthy(ctx context.Context, cli *client.Client, pollInterval time.Duration) error {
	return WaitForHealthy(ctx, cli, pinchyenv.LLMProxyContainerName, pollInterval)
}

// LLMProxyStatus inspects the LLM proxy container and returns a Service
// describing its current state. The second return value is false when no
// llmproxy container exists.
func LLMProxyStatus(ctx context.Context, cli *client.Client) (pinchyenv.Service, bool, error) {
	insp, err := cli.ContainerInspect(ctx, pinchyenv.LLMProxyContainerName)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return pinchyenv.Service{}, false, nil
		}
		return pinchyenv.Service{}, false, fmt.Errorf("inspecting llmproxy container: %w", err)
	}

	svc := pinchyenv.Service{
		Name: "llmproxy",
		// No host ports — internal only.
		Ports: []int{},
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

// LLMProxyLogs streams the LLM proxy container's logs to w. If follow is true,
// the stream stays open until the context is cancelled.
func LLMProxyLogs(ctx context.Context, cli *client.Client, follow bool, w io.Writer) error {
	rc, err := cli.ContainerLogs(ctx, pinchyenv.LLMProxyContainerName, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: true,
	})
	if err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("llmproxy container %q not found — run `pinchy init` first", pinchyenv.LLMProxyContainerName)
		}
		return fmt.Errorf("fetching llmproxy logs: %w", err)
	}
	defer rc.Close()

	_, err = io.Copy(w, rc)
	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		return fmt.Errorf("reading llmproxy logs: %w", err)
	}
	return nil
}
