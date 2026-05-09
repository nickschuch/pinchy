package dockerx

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// EnsureVolume creates the named volume with the given labels if it does not
// already exist. Existing volumes are left untouched.
func EnsureVolume(ctx context.Context, cli *client.Client, name string, labels map[string]string) error {
	_, err := cli.VolumeInspect(ctx, name)
	if err == nil {
		return nil
	}
	if !errdefs.IsNotFound(err) {
		return fmt.Errorf("inspecting volume %q: %w", name, err)
	}
	_, err = cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   name,
		Labels: labels,
	})
	if err != nil {
		return fmt.Errorf("creating volume %q: %w", name, err)
	}
	return nil
}

// RemoveVolume removes the named volume, ignoring NotFound.
func RemoveVolume(ctx context.Context, cli *client.Client, name string) error {
	err := cli.VolumeRemove(ctx, name, true)
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return nil
	}
	return fmt.Errorf("removing volume %q: %w", name, err)
}
