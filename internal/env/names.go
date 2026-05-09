package env

import (
	"fmt"
	"regexp"
)

// nameRE constrains environment names to a DNS-friendly subset that is also
// safe to embed in Docker container, volume, and network names.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// ValidateName returns an error if name is not a legal pinchy environment
// identifier.
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid environment name %q: must match %s", name, nameRE.String())
	}
	return nil
}

// AgentContainerName returns the Docker container name for the agent in env.
func AgentContainerName(env string) string {
	return fmt.Sprintf("pinchy-%s-agent", env)
}

// DockerContainerName returns the Docker container name for the dind daemon
// in env.
func DockerContainerName(env string) string {
	return fmt.Sprintf("pinchy-%s-docker", env)
}

// DataVolumeName returns the named volume that backs the dind daemon's image
// store for env.
func DataVolumeName(env string) string {
	return fmt.Sprintf("pinchy-%s-data", env)
}

// SockVolumeName returns the named volume that carries the rootless dockerd
// socket between the docker and agent containers for env.
func SockVolumeName(env string) string {
	return fmt.Sprintf("pinchy-%s-sock", env)
}

// NetworkName returns the user-defined bridge network name for env.
func NetworkName(env string) string {
	return fmt.Sprintf("pinchy-%s", env)
}

// SharedNetworkName is the bridge network that the proxy and all agent
// containers join so the proxy can route traffic to any environment.
const SharedNetworkName = "pinchy-shared"

// ProxyContainerName is the well-known name of the single shared Traefik
// reverse-proxy container.
const ProxyContainerName = "pinchy-proxy"

// ContainerNameFor returns the container name corresponding to a role within
// env, or an empty string if role is unknown.
func ContainerNameFor(env, role string) string {
	switch role {
	case RoleAgent:
		return AgentContainerName(env)
	case RoleDocker:
		return DockerContainerName(env)
	default:
		return ""
	}
}
