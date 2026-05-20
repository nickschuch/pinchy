package env

import (
	"fmt"
	"regexp"
)

// nameRE constrains environment names to a DNS-friendly subset that is also
// safe to embed in Docker container, volume, and network names.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// reservedNames is the set of identifiers that pinchy uses for its own global
// services. An environment may not be given one of these names because doing
// so would cause its containers and Traefik host rule to clash with a built-in
// service.
var reservedNames = map[string]struct{}{
	"proxy":    {},
	"console":  {},
	"llmproxy": {},
}

// ValidateName returns an error if name is not a legal pinchy environment
// identifier.
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid environment name %q: must match %s", name, nameRE.String())
	}
	if _, reserved := reservedNames[name]; reserved {
		return fmt.Errorf("invalid environment name %q: reserved for internal use", name)
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

// ConsoleContainerName is the well-known name of the pinchy discovery console
// container. It serves an HTML dashboard at
// http://console.pinchy.localhost:8080 via the Traefik proxy.
const ConsoleContainerName = "pinchy-console"

// LLMProxyContainerName is the well-known name of the shared LiteLLM-based LLM
// proxy container. It exposes the Anthropic passthrough API internally on port
// 4000 and its admin UI at http://llmproxy.pinchy.localhost:8080 via the
// Traefik proxy.
const LLMProxyContainerName = "pinchy-llmproxy"

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
