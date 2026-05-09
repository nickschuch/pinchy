package dockerx

import (
	"context"
	"fmt"

	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// EnsureImageLocal verifies that the named image reference exists in the
// local Docker image store. It does not pull from a registry — pinchy v1 is
// strictly local-image-only and expects images to be built ahead of time
// (e.g. via "make images").
func EnsureImageLocal(ctx context.Context, cli *client.Client, ref string) error {
	_, err := cli.ImageInspect(ctx, ref)
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return fmt.Errorf("image %q not found locally; build it with: make images", ref)
	}
	return fmt.Errorf("inspecting image %q: %w", ref, err)
}
