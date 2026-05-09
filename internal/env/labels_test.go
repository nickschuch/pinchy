package env

import (
	"strings"
	"testing"
)

// TestProxyTraefikLabelsAgent verifies that the agent labels include routers,
// services, healthcheck config, and the shared-network annotation for every
// expected port. Healthcheck paths are per-port (e.g. :4096 uses /global/health
// to match the opencode web server, while :8080/:3000 use /healthz).
func TestProxyTraefikLabelsAgent(t *testing.T) {
	envName := "myenv"
	labels := ProxyTraefikLabelsAgent(envName)

	// Container-level labels.
	mustHave(t, labels, "traefik.enable", "true")
	mustHave(t, labels, "traefik.docker.network", SharedNetworkName)

	// Per-port labels.
	for _, p := range proxyPorts {
		rtr := envName + "-" + p.ep
		svc := envName + "-" + p.ep
		prefix := "traefik.http"

		mustHave(t, labels, prefix+".routers."+rtr+".rule", "Host(`myenv.pinchy.localhost`)")
		mustHave(t, labels, prefix+".routers."+rtr+".entrypoints", p.ep)
		mustHave(t, labels, prefix+".routers."+rtr+".service", svc)
		mustHave(t, labels, prefix+".services."+svc+".loadbalancer.server.port", p.port)
		mustHave(t, labels, prefix+".services."+svc+".loadbalancer.healthcheck.path", p.healthPath)
		mustHave(t, labels, prefix+".services."+svc+".loadbalancer.healthcheck.interval", "5s")
		mustHave(t, labels, prefix+".services."+svc+".loadbalancer.healthcheck.timeout", "2s")
	}
}

// TestProxyTraefikLabelsDocker verifies that the dind labels include the
// service-level config (server.port + healthcheck) and the shared-network
// annotation, but do NOT include router labels.
//
// The service-level labels must be byte-identical to the agent's labels;
// see TestProxyTraefikLabelsAgentDockerServiceLabelsMatch for that assertion.
func TestProxyTraefikLabelsDocker(t *testing.T) {
	envName := "myenv"
	labels := ProxyTraefikLabelsDocker(envName)

	// Container-level labels.
	mustHave(t, labels, "traefik.enable", "true")
	mustHave(t, labels, "traefik.docker.network", SharedNetworkName)

	// Per-port: service config must be present with the correct per-port
	// healthcheck path.
	for _, p := range proxyPorts {
		svc := envName + "-" + p.ep
		mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.server.port", p.port)
		mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.healthcheck.path", p.healthPath)
		mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.healthcheck.interval", "5s")
		mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.healthcheck.timeout", "2s")
	}

	// Router labels must NOT appear on the dind container — routers are
	// declared solely by the agent labels.
	for k := range labels {
		if strings.Contains(k, ".routers.") {
			t.Errorf("unexpected router label in docker labels: %q", k)
		}
	}
}

// TestProxyPortHealthPaths verifies that the per-port health paths are set to
// the expected values so that Traefik healthchecks match what actually runs on
// each port (opencode web on :4096 uses /global/health; user services use /healthz).
func TestProxyPortHealthPaths(t *testing.T) {
	expected := map[string]string{
		"p8080": "/healthz",
		"p4096": "/global/health",
		"p3000": "/healthz",
	}
	for _, p := range proxyPorts {
		want, ok := expected[p.ep]
		if !ok {
			t.Errorf("unexpected proxyPort entry %q — update this test", p.ep)
			continue
		}
		if p.healthPath != want {
			t.Errorf("proxyPort %q: healthPath = %q, want %q", p.ep, p.healthPath, want)
		}
	}
}

// TestProxyTraefikLabelsAgentDockerServiceLabelsMatch is a regression test for
// the "Service defined multiple times with different configurations" Traefik
// error. It verifies that every traefik.http.services.* label is byte-identical
// between the agent and docker label sets. Any difference causes Traefik to
// drop the service, resulting in 404 responses.
func TestProxyTraefikLabelsAgentDockerServiceLabelsMatch(t *testing.T) {
	envName := "myenv"
	agentLabels := ProxyTraefikLabelsAgent(envName)
	dockerLabels := ProxyTraefikLabelsDocker(envName)

	const svcPrefix = "traefik.http.services."

	// Every service label on the agent must appear on docker with the same value.
	for k, av := range agentLabels {
		if !strings.HasPrefix(k, svcPrefix) {
			continue
		}
		dv, ok := dockerLabels[k]
		if !ok {
			t.Errorf("service label %q present on agent but missing on docker", k)
			continue
		}
		if av != dv {
			t.Errorf("service label %q mismatch: agent=%q docker=%q", k, av, dv)
		}
	}

	// Every service label on docker must appear on agent with the same value.
	for k, dv := range dockerLabels {
		if !strings.HasPrefix(k, svcPrefix) {
			continue
		}
		av, ok := agentLabels[k]
		if !ok {
			t.Errorf("service label %q present on docker but missing on agent", k)
			continue
		}
		if dv != av {
			t.Errorf("service label %q mismatch: docker=%q agent=%q", k, dv, av)
		}
	}
}

// TestProxyTraefikLabelsServiceNamesMatch verifies that the service names used
// in agent router labels match the service names in the docker server-port
// labels, which is the mechanism that causes Traefik to merge them into a
// single service with two backends.
func TestProxyTraefikLabelsServiceNamesMatch(t *testing.T) {
	envName := "testenv"
	agentLabels := ProxyTraefikLabelsAgent(envName)
	dockerLabels := ProxyTraefikLabelsDocker(envName)

	for _, p := range proxyPorts {
		svc := envName + "-" + p.ep
		agentPort := agentLabels["traefik.http.services."+svc+".loadbalancer.server.port"]
		dockerPort := dockerLabels["traefik.http.services."+svc+".loadbalancer.server.port"]

		if agentPort == "" {
			t.Errorf("agent labels missing server.port for service %q", svc)
		}
		if dockerPort == "" {
			t.Errorf("docker labels missing server.port for service %q", svc)
		}
		if agentPort != dockerPort {
			t.Errorf("port mismatch for service %q: agent=%q docker=%q", svc, agentPort, dockerPort)
		}
	}
}

// TestProxyTraefikLabelsEnvIsolation checks that labels for two different
// environments do not share router or service names.
func TestProxyTraefikLabelsEnvIsolation(t *testing.T) {
	aLabels := ProxyTraefikLabelsAgent("alpha")
	bLabels := ProxyTraefikLabelsAgent("beta")

	for k := range aLabels {
		if _, clash := bLabels[k]; clash && strings.Contains(k, "routers.") {
			t.Errorf("router label key %q appears in both alpha and beta environments", k)
		}
	}
}

// mustHave is a helper that fails the test if labels[key] != want.
func mustHave(t *testing.T, labels map[string]string, key, want string) {
	t.Helper()
	got, ok := labels[key]
	if !ok {
		t.Errorf("label %q missing (want %q)", key, want)
		return
	}
	if got != want {
		t.Errorf("label %q = %q, want %q", key, got, want)
	}
}
