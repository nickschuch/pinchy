package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a test helper that writes content to a file under dir.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	return path
}

// --------------------------------------------------------------------------
// DefaultPath
// --------------------------------------------------------------------------

func TestDefaultPath_PinchyConfigEnv(t *testing.T) {
	t.Setenv("PINCHY_CONFIG", "/custom/path/config.yaml")
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/custom/path/config.yaml" {
		t.Errorf("got %q, want /custom/path/config.yaml", got)
	}
}

func TestDefaultPath_XDGConfigHome(t *testing.T) {
	t.Setenv("PINCHY_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/xdg/config/pinchy/config.yaml"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultPath_FallbackToHome(t *testing.T) {
	t.Setenv("PINCHY_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "pinchy", "config.yaml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --------------------------------------------------------------------------
// Load
// --------------------------------------------------------------------------

func TestLoad_MissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Env) != 0 {
		t.Errorf("expected empty Env, got %v", cfg.Env)
	}
}

func TestLoad_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", "")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Env) != 0 {
		t.Errorf("expected empty Env, got %v", cfg.Env)
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
env:
  ANTHROPIC_API_KEY: sk-ant-global
  GITHUB_TOKEN: ghp_global

environments:
  myenv:
    env:
      ANTHROPIC_API_KEY: sk-ant-override
      AWS_PROFILE: myenv-profile
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Env["ANTHROPIC_API_KEY"] != "sk-ant-global" {
		t.Errorf("global ANTHROPIC_API_KEY = %q, want sk-ant-global", cfg.Env["ANTHROPIC_API_KEY"])
	}
	if cfg.Env["GITHUB_TOKEN"] != "ghp_global" {
		t.Errorf("global GITHUB_TOKEN = %q, want ghp_global", cfg.Env["GITHUB_TOKEN"])
	}
	ec := cfg.Environments["myenv"]
	if ec.Env["ANTHROPIC_API_KEY"] != "sk-ant-override" {
		t.Errorf("myenv ANTHROPIC_API_KEY = %q, want sk-ant-override", ec.Env["ANTHROPIC_API_KEY"])
	}
	if ec.Env["AWS_PROFILE"] != "myenv-profile" {
		t.Errorf("myenv AWS_PROFILE = %q, want myenv-profile", ec.Env["AWS_PROFILE"])
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", "env: [not a map")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestLoad_InvalidKeyWithEquals(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", "env:\n  \"KEY=BAD\": value\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for key containing '=', got nil")
	}
}

func TestLoad_InvalidEmptyKey(t *testing.T) {
	dir := t.TempDir()
	// YAML allows empty string keys.
	path := writeFile(t, dir, "config.yaml", "env:\n  \"\": value\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for empty key, got nil")
	}
}

func TestLoad_InvalidKeyInPerEnvBlock(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
environments:
  myenv:
    env:
      "BAD=KEY": value
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for bad key in per-env block, got nil")
	}
}

// --------------------------------------------------------------------------
// EnvFor
// --------------------------------------------------------------------------

func TestEnvFor_GlobalOnly(t *testing.T) {
	cfg := &Config{
		Env: map[string]string{
			"FOO": "foo-global",
			"BAR": "bar-global",
		},
	}

	got := cfg.EnvFor("anyenv")
	if got["FOO"] != "foo-global" {
		t.Errorf("FOO = %q, want foo-global", got["FOO"])
	}
	if got["BAR"] != "bar-global" {
		t.Errorf("BAR = %q, want bar-global", got["BAR"])
	}
}

func TestEnvFor_PerEnvOverridesGlobal(t *testing.T) {
	cfg := &Config{
		Env: map[string]string{
			"FOO": "foo-global",
			"BAR": "bar-global",
		},
		Environments: map[string]EnvConfig{
			"myenv": {
				Env: map[string]string{
					"FOO": "foo-override",
					"BAZ": "baz-new",
				},
			},
		},
	}

	got := cfg.EnvFor("myenv")
	if got["FOO"] != "foo-override" {
		t.Errorf("FOO = %q, want foo-override", got["FOO"])
	}
	if got["BAR"] != "bar-global" {
		t.Errorf("BAR = %q, want bar-global", got["BAR"])
	}
	if got["BAZ"] != "baz-new" {
		t.Errorf("BAZ = %q, want baz-new", got["BAZ"])
	}
}

func TestEnvFor_UnknownEnvGetsOnlyGlobal(t *testing.T) {
	cfg := &Config{
		Env: map[string]string{
			"GLOBAL": "yes",
		},
		Environments: map[string]EnvConfig{
			"other": {Env: map[string]string{"OTHER": "val"}},
		},
	}

	got := cfg.EnvFor("notregistered")
	if got["GLOBAL"] != "yes" {
		t.Errorf("GLOBAL = %q, want yes", got["GLOBAL"])
	}
	if _, ok := got["OTHER"]; ok {
		t.Error("OTHER should not be present for an unrelated env")
	}
}

func TestEnvFor_ReturnsFreshCopy(t *testing.T) {
	cfg := &Config{
		Env: map[string]string{"KEY": "val"},
	}
	a := cfg.EnvFor("env1")
	a["KEY"] = "mutated"

	b := cfg.EnvFor("env1")
	if b["KEY"] != "val" {
		t.Errorf("EnvFor returned same map; mutation leaked: KEY = %q", b["KEY"])
	}
}

func TestEnvFor_EmptyConfig(t *testing.T) {
	cfg := &Config{}
	got := cfg.EnvFor("whatever")
	if got == nil {
		t.Error("expected non-nil map for empty config")
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

// --------------------------------------------------------------------------
// validateKey
// --------------------------------------------------------------------------

func TestValidateKey(t *testing.T) {
	cases := []struct {
		key     string
		wantErr bool
	}{
		{"VALID_KEY", false},
		{"valid_lower", false},
		{"KEY123", false},
		{"", true},
		{"KEY=BAD", true},
		{"KEY\x00BAD", true},
	}
	for _, tc := range cases {
		err := validateKey(tc.key)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateKey(%q) error = %v, wantErr %v", tc.key, err, tc.wantErr)
		}
	}
}
