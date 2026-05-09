package dockerx

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// CreateAndStart creates a container with the given configuration and starts
// it. It returns the new container ID. The networkName, if non-empty, is
// attached at create time so the container joins it before its first start.
func CreateAndStart(
	ctx context.Context,
	cli *client.Client,
	name string,
	config *container.Config,
	hostConfig *container.HostConfig,
	networkName string,
) (string, error) {
	var netConfig *network.NetworkingConfig
	if networkName != "" {
		netConfig = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				networkName: {},
			},
		}
	}
	resp, err := cli.ContainerCreate(ctx, config, hostConfig, netConfig, nil, name)
	if err != nil {
		return "", fmt.Errorf("creating container %q: %w", name, err)
	}
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("starting container %q: %w", name, err)
	}
	return resp.ID, nil
}

// StopContainer stops the container by name, ignoring NotFound and
// already-stopped errors.
func StopContainer(ctx context.Context, cli *client.Client, name string) error {
	err := cli.ContainerStop(ctx, name, container.StopOptions{})
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return nil
	}
	return fmt.Errorf("stopping container %q: %w", name, err)
}

// StartContainer starts an existing container by name.
func StartContainer(ctx context.Context, cli *client.Client, name string) error {
	err := cli.ContainerStart(ctx, name, container.StartOptions{})
	if err == nil {
		return nil
	}
	return fmt.Errorf("starting container %q: %w", name, err)
}

// RemoveContainer removes the container by name. force=true sends SIGKILL if
// the container is still running.
func RemoveContainer(ctx context.Context, cli *client.Client, name string, force bool) error {
	err := cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: force})
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return nil
	}
	return fmt.Errorf("removing container %q: %w", name, err)
}

// HasTraefikLabels reports whether the container identified by containerID has
// the traefik.enable label set to "true". This is used to detect environments
// created before dual-backend routing was introduced.
func HasTraefikLabels(ctx context.Context, cli *client.Client, containerID string) bool {
	insp, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return false
	}
	return insp.Config != nil && insp.Config.Labels["traefik.enable"] == "true"
}
