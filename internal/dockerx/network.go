package dockerx

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"

	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

// EnsureNetwork creates a user-defined bridge network with the given labels
// if it does not already exist.
func EnsureNetwork(ctx context.Context, cli *client.Client, name string, labels map[string]string) error {
	_, err := cli.NetworkInspect(ctx, name, network.InspectOptions{})
	if err == nil {
		return nil
	}
	if !errdefs.IsNotFound(err) {
		return fmt.Errorf("inspecting network %q: %w", name, err)
	}
	_, err = cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
		Labels: labels,
	})
	if err != nil {
		return fmt.Errorf("creating network %q: %w", name, err)
	}
	return nil
}

// EnsureSharedNetwork creates the pinchy-shared bridge network if it does not
// already exist. This network connects the proxy container to all environment
// agent containers.
func EnsureSharedNetwork(ctx context.Context, cli *client.Client, version string) error {
	labels := pinchyenv.ServiceLabels(pinchyenv.RoleSharedNetwork, version, "")
	return EnsureNetwork(ctx, cli, pinchyenv.SharedNetworkName, labels)
}

// ConnectToSharedNetwork attaches containerID to the pinchy-shared network.
// It is idempotent: "already connected" errors are silently ignored.
func ConnectToSharedNetwork(ctx context.Context, cli *client.Client, containerID string) error {
	err := cli.NetworkConnect(ctx, pinchyenv.SharedNetworkName, containerID, nil)
	if err == nil {
		return nil
	}
	// Docker returns a 403 with "already exists" in the message when the
	// container is already a member of the network.
	if errdefs.IsForbidden(err) {
		return nil
	}
	return fmt.Errorf("connecting container %q to shared network: %w", containerID, err)
}

// RemoveNetwork removes the named network, ignoring NotFound.
func RemoveNetwork(ctx context.Context, cli *client.Client, name string) error {
	err := cli.NetworkRemove(ctx, name)
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return nil
	}
	return fmt.Errorf("removing network %q: %w", name, err)
}
