package env

import "time"

// Service describes a discovered pinchy global service (e.g. the proxy).
// Unlike Environment, a Service is not scoped to a single environment.
type Service struct {
	// Name is the well-known service identifier (e.g. "proxy").
	Name string
	// Status is the human-readable Docker status string.
	Status string
	// Health is the Docker health status ("healthy", "unhealthy", "starting",
	// or "" when no healthcheck is configured).
	Health string
	// Ports lists the host ports the service is bound to.
	Ports []int
	// Created is the timestamp the container was labelled with.
	Created time.Time
}

// Environment describes a discovered pinchy environment.
type Environment struct {
	// Name is the user-facing identifier (matches LabelEnv).
	Name string
	// Workdir is the absolute host path bind-mounted into the agent (from
	// LabelWorkdir on the agent container). Empty if unknown.
	Workdir string
	// Created is the timestamp the agent container was labelled with.
	Created time.Time
	// Version is the pinchy version that created the agent.
	Version string
	// AgentStatus is the human-readable Docker status string for the agent
	// container ("Up 5 minutes", "Exited (0)", etc.).
	AgentStatus string
	// AgentRunning is true if the agent container is currently running.
	AgentRunning bool
	// DockerStatus is the equivalent for the docker daemon container.
	DockerStatus string
	// DockerHealth is one of "healthy", "unhealthy", "starting", or "" (no
	// healthcheck reported).
	DockerHealth string
	// AgentContainerID is the full Docker container ID (for direct API calls).
	AgentContainerID string
	// DockerContainerID is the full Docker container ID for the daemon.
	DockerContainerID string
}
