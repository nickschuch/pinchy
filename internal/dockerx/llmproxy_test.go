package dockerx

import (
	"strings"
	"testing"

	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

func TestLLMProxyContainerConfig_NoPortBindings(t *testing.T) {
	cfg := LLMProxyConfig{
		Image:           "ghcr.io/nickschuch/pinchy-llmproxy:latest",
		Version:         "test",
		AnthropicAPIKey: "sk-ant-test",
	}
	_, hostCfg := llmproxyContainerConfig(cfg)

	if len(hostCfg.PortBindings) != 0 {
		t.Errorf("expected no port bindings, got %v", hostCfg.PortBindings)
	}
}

func TestLLMProxyContainerConfig_NetworkMode(t *testing.T) {
	cfg := LLMProxyConfig{
		Image:           "ghcr.io/nickschuch/pinchy-llmproxy:latest",
		Version:         "test",
		AnthropicAPIKey: "sk-ant-test",
	}
	_, hostCfg := llmproxyContainerConfig(cfg)

	want := pinchyenv.SharedNetworkName
	if string(hostCfg.NetworkMode) != want {
		t.Errorf("NetworkMode = %q, want %q", hostCfg.NetworkMode, want)
	}
}

func TestLLMProxyContainerConfig_Healthcheck(t *testing.T) {
	cfg := LLMProxyConfig{
		Image:           "ghcr.io/nickschuch/pinchy-llmproxy:latest",
		Version:         "test",
		AnthropicAPIKey: "sk-ant-test",
	}
	containerCfg, _ := llmproxyContainerConfig(cfg)

	if containerCfg.Healthcheck == nil {
		t.Fatal("expected healthcheck to be set")
	}
	found := false
	for _, arg := range containerCfg.Healthcheck.Test {
		if strings.Contains(arg, "/health/liveliness") {
			found = true
		}
	}
	if !found {
		t.Errorf("healthcheck does not probe /health/liveliness: %v", containerCfg.Healthcheck.Test)
	}
}

func TestLLMProxyContainerConfig_KeyHashLabel(t *testing.T) {
	key := "sk-ant-secret"
	cfg := LLMProxyConfig{
		Image:           "ghcr.io/nickschuch/pinchy-llmproxy:latest",
		Version:         "test",
		AnthropicAPIKey: key,
	}
	containerCfg, _ := llmproxyContainerConfig(cfg)

	hash, ok := containerCfg.Labels[pinchyenv.LabelLLMProxyKeyHash]
	if !ok {
		t.Fatalf("label %q missing", pinchyenv.LabelLLMProxyKeyHash)
	}
	if hash == "" {
		t.Error("key hash label is empty")
	}
	// Verify the key itself does not appear in any label.
	for k, v := range containerCfg.Labels {
		if strings.Contains(v, key) {
			t.Errorf("label %q contains the raw API key — must only store the hash", k)
		}
	}
}

func TestLLMProxyContainerConfig_KeyNotInLabelValues(t *testing.T) {
	key := "sk-ant-super-secret-key"
	cfg := LLMProxyConfig{
		Image:           "ghcr.io/nickschuch/pinchy-llmproxy:latest",
		Version:         "test",
		AnthropicAPIKey: key,
	}
	containerCfg, _ := llmproxyContainerConfig(cfg)

	for k, v := range containerCfg.Labels {
		if strings.Contains(v, key) {
			t.Errorf("label %q = %q exposes the raw API key", k, v)
		}
	}
}

func TestLLMProxyContainerConfig_TraefikLabels(t *testing.T) {
	cfg := LLMProxyConfig{
		Image:           "ghcr.io/nickschuch/pinchy-llmproxy:latest",
		Version:         "test",
		AnthropicAPIKey: "sk-ant-test",
	}
	containerCfg, _ := llmproxyContainerConfig(cfg)

	// Must have traefik.enable=true.
	if v := containerCfg.Labels["traefik.enable"]; v != "true" {
		t.Errorf("traefik.enable = %q, want true", v)
	}
	// Must have a router rule pointing at llmproxy.pinchy.localhost.
	found := false
	for k, v := range containerCfg.Labels {
		if strings.Contains(k, ".routers.") && strings.Contains(k, ".rule") {
			if strings.Contains(v, "llmproxy.pinchy.localhost") {
				found = true
			}
		}
	}
	if !found {
		t.Error("no Traefik router rule containing llmproxy.pinchy.localhost found in labels")
	}
}

func TestKeyHash_Deterministic(t *testing.T) {
	h1 := keyHash("sk-ant-test")
	h2 := keyHash("sk-ant-test")
	if h1 != h2 {
		t.Errorf("keyHash is not deterministic: %q != %q", h1, h2)
	}
}

func TestKeyHash_DifferentKeys(t *testing.T) {
	h1 := keyHash("sk-ant-test")
	h2 := keyHash("sk-ant-other")
	if h1 == h2 {
		t.Error("keyHash produced same value for different keys")
	}
}
