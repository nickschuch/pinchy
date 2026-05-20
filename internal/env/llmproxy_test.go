package env

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

// litellmConfig is a minimal struct for parsing the fields we care about from
// images/llmproxy/config.yaml.
type litellmConfig struct {
	GeneralSettings struct {
		MasterKey string `yaml:"master_key"`
	} `yaml:"general_settings"`
}

// repoRoot resolves the repository root relative to this test file's location.
// internal/env/ is two levels below the repo root.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file = …/internal/env/llmproxy_test.go
	// go up two directories to reach the repo root
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// TestLLMProxyTokenMatchesConfig is a drift guard: it fails if
// LLMProxyToken (in Go source) diverges from general_settings.master_key in
// images/llmproxy/config.yaml. Both must always be identical because the agent
// image bakes LLMProxyToken as its ANTHROPIC_API_KEY, and LiteLLM uses
// master_key to validate incoming requests.
func TestLLMProxyTokenMatchesConfig(t *testing.T) {
	cfgPath := filepath.Join(repoRoot(t), "images", "llmproxy", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("reading %s: %v", cfgPath, err)
	}

	var cfg litellmConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parsing %s: %v", cfgPath, err)
	}

	if cfg.GeneralSettings.MasterKey != LLMProxyToken {
		t.Errorf("token mismatch:\n  Go constant LLMProxyToken     = %q\n  config.yaml master_key         = %q\n\nUpdate one to match the other.",
			LLMProxyToken, cfg.GeneralSettings.MasterKey)
	}
}

// TestLLMProxyAgentDockerfileEnv is a drift guard: it fails if the
// ANTHROPIC_API_KEY ENV line in images/agent/Dockerfile is not set to
// LLMProxyToken.
func TestLLMProxyAgentDockerfileEnv(t *testing.T) {
	dfPath := filepath.Join(repoRoot(t), "images", "agent", "Dockerfile")
	data, err := os.ReadFile(dfPath)
	if err != nil {
		t.Fatalf("reading %s: %v", dfPath, err)
	}

	want := "ENV ANTHROPIC_API_KEY=" + LLMProxyToken
	content := string(data)
	if !contains(content, want) {
		t.Errorf("Dockerfile does not contain expected line:\n  %q\n\nEnsure images/agent/Dockerfile has:\n  %s", want, want)
	}
}

// TestLLMProxyAgentOpenCodeJSON is a drift guard: it fails if
// images/agent/opencode.json does not contain the expected baseURL.
func TestLLMProxyAgentOpenCodeJSON(t *testing.T) {
	jsonPath := filepath.Join(repoRoot(t), "images", "agent", "opencode.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("reading %s: %v", jsonPath, err)
	}

	content := string(data)
	if !contains(content, LLMProxyBaseURL) {
		t.Errorf("opencode.json does not contain expected baseURL:\n  %q\n\nEnsure images/agent/opencode.json has provider.anthropic.options.baseURL set to:\n  %s", LLMProxyBaseURL, LLMProxyBaseURL)
	}
}

// contains is a simple substring check (avoids importing strings in a test file
// that already imports several packages).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
