package env

import (
	"strings"
	"testing"
)

// TestConsoleTraefikLabels verifies that the console container's Traefik labels
// include a router on the p8080 entrypoint with the correct host rule, a
// service with the expected server port, and active healthcheck config.
func TestConsoleTraefikLabels(t *testing.T) {
	labels := ConsoleTraefikLabels()

	mustHave(t, labels, "traefik.enable", "true")
	mustHave(t, labels, "traefik.docker.network", SharedNetworkName)

	const rtr = "console-p8080"
	const svc = "console-p8080"

	mustHave(t, labels, "traefik.http.routers."+rtr+".rule", "Host(`console.pinchy.localhost`)")
	mustHave(t, labels, "traefik.http.routers."+rtr+".entrypoints", "p8080")
	mustHave(t, labels, "traefik.http.routers."+rtr+".service", svc)
	mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.server.port", "8080")
	mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.healthcheck.path", "/healthz")
	mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.healthcheck.interval", "5s")
	mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.healthcheck.timeout", "2s")
}

// TestConsoleTraefikLabelsNoEnvRouters verifies that the console labels do NOT
// contain router entries for per-environment routes (e.g. myenv-p8080). The
// console is a global service with a single fixed hostname.
func TestConsoleTraefikLabelsNoEnvRouters(t *testing.T) {
	labels := ConsoleTraefikLabels()
	for k := range labels {
		if strings.Contains(k, ".routers.") && !strings.Contains(k, "console-p8080") {
			t.Errorf("unexpected non-console router label: %q", k)
		}
	}
}

// TestValidateNameRejectsConsole verifies that "console" is rejected as a
// pinchy environment name because it is reserved for the discovery console
// global service.
func TestValidateNameRejectsConsole(t *testing.T) {
	if err := ValidateName("console"); err == nil {
		t.Error("ValidateName(\"console\") returned nil, want error")
	}
}

// TestValidateNameRejectsProxy verifies that "proxy" is still rejected.
func TestValidateNameRejectsProxy(t *testing.T) {
	if err := ValidateName("proxy"); err == nil {
		t.Error("ValidateName(\"proxy\") returned nil, want error")
	}
}

// TestValidateNameAcceptsOthers verifies that ordinary names are still accepted.
func TestValidateNameAcceptsOthers(t *testing.T) {
	for _, name := range []string{"myenv", "dev", "staging", "my-project"} {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}
}

// TestValidateNameRejectsLLMProxy verifies that "llmproxy" is rejected because
// it is reserved for the shared LLM proxy global service.
func TestValidateNameRejectsLLMProxy(t *testing.T) {
	if err := ValidateName("llmproxy"); err == nil {
		t.Error("ValidateName(\"llmproxy\") returned nil, want error")
	}
}

// TestLLMProxyTraefikLabels verifies that the LLM proxy Traefik labels include
// the correct router, service, and healthcheck configuration.
func TestLLMProxyTraefikLabels(t *testing.T) {
	labels := LLMProxyTraefikLabels()

	mustHave(t, labels, "traefik.enable", "true")
	mustHave(t, labels, "traefik.docker.network", SharedNetworkName)

	const rtr = "llmproxy-p8080"
	const svc = "llmproxy-p8080"

	mustHave(t, labels, "traefik.http.routers."+rtr+".rule", "Host(`llmproxy.pinchy.localhost`)")
	mustHave(t, labels, "traefik.http.routers."+rtr+".entrypoints", "p8080")
	mustHave(t, labels, "traefik.http.routers."+rtr+".service", svc)
	mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.server.port", "4000")
	mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.healthcheck.path", "/health/liveliness")
	mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.healthcheck.interval", "5s")
	mustHave(t, labels, "traefik.http.services."+svc+".loadbalancer.healthcheck.timeout", "2s")
}

// TestLLMProxyTraefikLabelsNoEnvRouters verifies that the LLM proxy labels do
// NOT contain per-environment router entries.
func TestLLMProxyTraefikLabelsNoEnvRouters(t *testing.T) {
	labels := LLMProxyTraefikLabels()
	for k := range labels {
		if strings.Contains(k, ".routers.") && !strings.Contains(k, "llmproxy-p8080") {
			t.Errorf("unexpected non-llmproxy router label: %q", k)
		}
	}
}
