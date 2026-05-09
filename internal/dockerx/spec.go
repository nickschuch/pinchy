package dockerx

import (
	"fmt"
	"sort"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

// AgentSpec describes everything required to create the agent container of a
// pinchy environment.
type AgentSpec struct {
	Image       string
	HostWorkdir string // bind-mounted at /data
	SockVolume  string // shared with the docker container
	Network     string
	Labels      map[string]string
	// Env holds additional environment variables to inject into the agent
	// container.  Values here are layered on top of the baseline entries
	// (e.g. DOCKER_HOST); if a key appears in both, the caller-supplied value
	// wins.
	Env map[string]string
}

// DockerSpec describes the dind-rootless docker daemon container of a pinchy
// environment.
type DockerSpec struct {
	Image      string
	DataVolume string
	SockVolume string
	Network    string
	Labels     map[string]string
}

// AgentConfig builds the container.Config + HostConfig pair for the agent.
func AgentConfig(s AgentSpec) (*container.Config, *container.HostConfig) {
	// Build the env slice.  Start with the baseline entry for DOCKER_HOST,
	// then append caller-supplied entries sorted alphabetically.  If the
	// caller explicitly sets DOCKER_HOST their value replaces the baseline.
	envMap := map[string]string{
		"DOCKER_HOST": "unix:///run/user/1000/docker.sock",
	}
	for k, v := range s.Env {
		envMap[k] = v
	}
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	envSlice := make([]string, 0, len(keys))
	for _, k := range keys {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, envMap[k]))
	}

	cfg := &container.Config{
		Image:      s.Image,
		WorkingDir: "/data",
		Env:        envSlice,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		OpenStdin:    true,
		Labels:       s.Labels,
	}
	host := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: s.HostWorkdir, Target: "/data"},
			{Type: mount.TypeVolume, Source: s.SockVolume, Target: "/run/user/1000"},
		},
		NetworkMode: container.NetworkMode(s.Network),
	}
	return cfg, host
}

// DockerConfig builds the container.Config + HostConfig pair for the dind
// daemon. The healthcheck mirrors the prior docker-compose definition.
func DockerConfig(s DockerSpec) (*container.Config, *container.HostConfig) {
	cfg := &container.Config{
		Image: s.Image,
		Env: []string{
			"DOCKER_TLS_CERTDIR=",
		},
		Labels: s.Labels,
		Healthcheck: &container.HealthConfig{
			Test:        []string{"CMD", "docker", "-H", "unix:///run/user/1000/docker.sock", "info"},
			Interval:    5 * time.Second,
			Timeout:     3 * time.Second,
			Retries:     20,
			StartPeriod: 10 * time.Second,
		},
	}
	host := &container.HostConfig{
		Privileged:    true,
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: s.DataVolume, Target: "/home/rootless/.local/share/docker"},
			{Type: mount.TypeVolume, Source: s.SockVolume, Target: "/run/user/1000"},
		},
		NetworkMode: container.NetworkMode(s.Network),
	}
	return cfg, host
}
