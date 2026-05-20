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
// LLMProxy
// --------------------------------------------------------------------------

func TestLoad_LLMProxyConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
llm_proxy:
  anthropic_api_key: sk-ant-real-key
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLMProxy == nil {
		t.Fatal("expected LLMProxy to be non-nil")
	}
	if cfg.LLMProxy.AnthropicAPIKey != "sk-ant-real-key" {
		t.Errorf("AnthropicAPIKey = %q, want sk-ant-real-key", cfg.LLMProxy.AnthropicAPIKey)
	}
}

func TestLoad_LLMProxyEnabled(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
llm_proxy:
  anthropic_api_key: sk-ant-real-key
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.LLMProxyEnabled() {
		t.Error("LLMProxyEnabled() = false, want true")
	}
}

func TestLoad_LLMProxyNotEnabled_NilBlock(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `env:
  FOO: bar
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLMProxyEnabled() {
		t.Error("LLMProxyEnabled() = true, want false for config with no llm_proxy block")
	}
}

func TestLoad_LLMProxyEmptyKey_Error(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
llm_proxy:
  anthropic_api_key: ""
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for empty anthropic_api_key, got nil")
	}
}

// --------------------------------------------------------------------------
// Mounts — Load / validate
// --------------------------------------------------------------------------

func TestLoad_MountsGlobal(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
mounts:
  - source: ~/.aws
    target: /home/skpr/.aws
  - source: ~/.skpr
    target: /home/skpr/.skpr
    mode: rw
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Mounts) != 2 {
		t.Fatalf("expected 2 global mounts, got %d", len(cfg.Mounts))
	}
	if cfg.Mounts[0].Source != "~/.aws" {
		t.Errorf("Mounts[0].Source = %q, want ~/.aws", cfg.Mounts[0].Source)
	}
	if cfg.Mounts[0].Target != "/home/skpr/.aws" {
		t.Errorf("Mounts[0].Target = %q, want /home/skpr/.aws", cfg.Mounts[0].Target)
	}
	if cfg.Mounts[0].Mode != "" {
		t.Errorf("Mounts[0].Mode = %q, want empty (ro default)", cfg.Mounts[0].Mode)
	}
	if cfg.Mounts[1].Mode != "rw" {
		t.Errorf("Mounts[1].Mode = %q, want rw", cfg.Mounts[1].Mode)
	}
}

func TestLoad_MountsPerEnv(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
environments:
  myenv:
    mounts:
      - source: ~/.ssh
        target: /home/skpr/.ssh
        mode: ro
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ec := cfg.Environments["myenv"]
	if len(ec.Mounts) != 1 {
		t.Fatalf("expected 1 per-env mount, got %d", len(ec.Mounts))
	}
	if ec.Mounts[0].Target != "/home/skpr/.ssh" {
		t.Errorf("Mounts[0].Target = %q, want /home/skpr/.ssh", ec.Mounts[0].Target)
	}
}

func TestMountsFor_GlobalOnly(t *testing.T) {
	cfg := &Config{
		Mounts: []Mount{
			{Source: "~/.aws", Target: "/home/skpr/.aws"},
			{Source: "~/.skpr", Target: "/home/skpr/.skpr"},
		},
	}
	got := cfg.MountsFor("anyenv")
	if len(got) != 2 {
		t.Fatalf("expected 2 mounts, got %d", len(got))
	}
	if got[0].Target != "/home/skpr/.aws" {
		t.Errorf("got[0].Target = %q, want /home/skpr/.aws", got[0].Target)
	}
}

func TestMountsFor_PerEnvReplacesGlobal(t *testing.T) {
	cfg := &Config{
		Mounts: []Mount{
			{Source: "~/.aws", Target: "/home/skpr/.aws", Mode: "ro"},
			{Source: "~/.skpr", Target: "/home/skpr/.skpr"},
		},
		Environments: map[string]EnvConfig{
			"myenv": {
				Mounts: []Mount{
					// Same target — should replace the global entry.
					{Source: "/custom/aws", Target: "/home/skpr/.aws", Mode: "rw"},
					// New target — should be appended.
					{Source: "~/.ssh", Target: "/home/skpr/.ssh"},
				},
			},
		},
	}
	got := cfg.MountsFor("myenv")
	if len(got) != 3 {
		t.Fatalf("expected 3 mounts after merge, got %d: %+v", len(got), got)
	}
	// Target /home/skpr/.aws should now point at /custom/aws with mode rw.
	var awsMount Mount
	for _, m := range got {
		if m.Target == "/home/skpr/.aws" {
			awsMount = m
		}
	}
	if awsMount.Source != "/custom/aws" {
		t.Errorf("aws mount Source = %q, want /custom/aws", awsMount.Source)
	}
	if awsMount.Mode != "rw" {
		t.Errorf("aws mount Mode = %q, want rw", awsMount.Mode)
	}
}

func TestMountsFor_PerEnvAppendsNew(t *testing.T) {
	cfg := &Config{
		Mounts: []Mount{
			{Source: "~/.aws", Target: "/home/skpr/.aws"},
		},
		Environments: map[string]EnvConfig{
			"myenv": {
				Mounts: []Mount{
					{Source: "~/.skpr", Target: "/home/skpr/.skpr"},
				},
			},
		},
	}
	got := cfg.MountsFor("myenv")
	if len(got) != 2 {
		t.Fatalf("expected 2 mounts, got %d", len(got))
	}
}

func TestMountsFor_UnknownEnvGetsOnlyGlobal(t *testing.T) {
	cfg := &Config{
		Mounts: []Mount{
			{Source: "~/.aws", Target: "/home/skpr/.aws"},
		},
		Environments: map[string]EnvConfig{
			"other": {
				Mounts: []Mount{
					{Source: "~/.skpr", Target: "/home/skpr/.skpr"},
				},
			},
		},
	}
	got := cfg.MountsFor("notregistered")
	if len(got) != 1 {
		t.Fatalf("expected 1 mount for unknown env, got %d", len(got))
	}
}

func TestMountsFor_ReturnsFreshCopy(t *testing.T) {
	cfg := &Config{
		Mounts: []Mount{
			{Source: "~/.aws", Target: "/home/skpr/.aws"},
		},
	}
	a := cfg.MountsFor("anyenv")
	a[0].Mode = "rw"
	b := cfg.MountsFor("anyenv")
	if b[0].Mode != "" {
		t.Errorf("MountsFor returned same slice; mutation leaked: Mode = %q", b[0].Mode)
	}
}

func TestLoad_InvalidMount_NoSource(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
mounts:
  - source: ""
    target: /home/skpr/.aws
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for empty source, got nil")
	}
}

func TestLoad_InvalidMount_NoTarget(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
mounts:
  - source: ~/.aws
    target: ""
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for empty target, got nil")
	}
}

func TestLoad_InvalidMount_RelativeTarget(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
mounts:
  - source: ~/.aws
    target: home/skpr/.aws
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for relative target, got nil")
	}
}

func TestLoad_InvalidMount_BadMode(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
mounts:
  - source: ~/.aws
    target: /home/skpr/.aws
    mode: rwx
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for invalid mode, got nil")
	}
}

func TestLoad_InvalidMount_ReservedTargetData(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
mounts:
  - source: /some/dir
    target: /data
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for reserved target /data, got nil")
	}
}

func TestLoad_InvalidMount_ReservedTargetSock(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
mounts:
  - source: /some/dir
    target: /run/user/1000
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for reserved target /run/user/1000, got nil")
	}
}

func TestLoad_InvalidMount_DuplicateTargetGlobal(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
mounts:
  - source: ~/.aws
    target: /home/skpr/.aws
  - source: /other/aws
    target: /home/skpr/.aws
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for duplicate target in global mounts, got nil")
	}
}

func TestLoad_InvalidMount_PerEnvRelativeTarget(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
environments:
  myenv:
    mounts:
      - source: ~/.aws
        target: relative/path
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for relative target in per-env mounts, got nil")
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
