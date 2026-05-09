// Package dockerx is a thin convenience layer over the official Docker
// engine SDK. It centralises client construction and exposes higher-level
// helpers tailored to pinchy's needs (creating fully-configured agent and
// docker daemon containers, multiplexing logs across an environment, etc.).
package dockerx

import (
	"github.com/docker/docker/client"
)

// NewClient returns a Docker engine client that honours DOCKER_HOST and the
// other standard environment variables.
func NewClient() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
}
