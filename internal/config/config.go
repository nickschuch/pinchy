// Package config loads and merges the pinchy global configuration file.
//
// The config file is looked up in the following order (first match wins):
//
//  1. Path supplied explicitly by the caller (e.g. from --config flag).
//  2. The PINCHY_CONFIG environment variable.
//  3. $XDG_CONFIG_HOME/pinchy/config.yaml (falls back to ~/.config/pinchy/config.yaml).
//
// A missing config file is not an error; Load returns an empty Config in that
// case.  A file that exists but is world- or group-readable triggers a warning
// written to stderr.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mount describes a host directory to bind-mount into the agent container.
//
// Source is a host path (~ is expanded to $HOME at apply time). Target must
// be an absolute path inside the container. Mode controls whether the mount
// is read-only ("ro", the default) or read-write ("rw").
type Mount struct {
	// Source is the host directory path to mount. A leading ~ or ~/ is
	// expanded to the current user's home directory at apply time.
	Source string `yaml:"source"`
	// Target is the absolute path inside the agent container where the
	// directory will be mounted. Must be absolute and non-empty.
	Target string `yaml:"target"`
	// Mode controls the mount access mode. Accepted values are "ro"
	// (read-only, the default when omitted) and "rw" (read-write).
	Mode string `yaml:"mode,omitempty"`
}

// EnvConfig holds per-environment configuration overrides.
type EnvConfig struct {
	// Env is a map of environment variable names to values injected into the
	// agent container for this specific environment. These values override the
	// top-level Env entries for the same keys.
	Env map[string]string `yaml:"env"`

	// Mounts is a list of host directories to bind-mount into the agent
	// container for this specific environment. Per-environment mounts are
	// merged with the global Mounts list; if a per-environment entry shares
	// the same Target as a global entry, the per-environment entry replaces
	// it.
	Mounts []Mount `yaml:"mounts,omitempty"`
}

// LLMProxyConfig holds configuration for the optional shared LLM proxy
// container (pinchy-llmproxy). When present and non-empty, pinchy starts a
// LiteLLM-based Anthropic proxy container that holds the real API key. Agent
// containers connect to it using the hardcoded shared token
// (env.LLMProxyToken) — they never receive the real key.
type LLMProxyConfig struct {
	// AnthropicAPIKey is the real Anthropic API key. It is injected into the
	// pinchy-llmproxy container at start time and is never passed to any agent
	// container.
	AnthropicAPIKey string `yaml:"anthropic_api_key"`
}

// Config is the top-level structure for the pinchy configuration file.
type Config struct {
	// Env is a map of environment variable names to values that are injected
	// into the agent container of every environment.
	Env map[string]string `yaml:"env"`

	// Mounts is the global list of host directories to bind-mount into every
	// agent container. Per-environment mounts (under environments.<name>.mounts)
	// are merged on top; entries sharing the same Target replace the global
	// entry for that Target.
	Mounts []Mount `yaml:"mounts,omitempty"`

	// Environments holds per-environment overrides keyed by environment name.
	// Values here are merged on top of the global Env entries, with
	// per-environment values winning.
	Environments map[string]EnvConfig `yaml:"environments"`

	// LLMProxy configures the optional shared LLM proxy container. If nil or
	// if AnthropicAPIKey is empty, the proxy is not started.
	LLMProxy *LLMProxyConfig `yaml:"llm_proxy,omitempty"`
}

// LLMProxyEnabled returns true when the config includes a non-empty
// LLM proxy Anthropic API key.
func (c *Config) LLMProxyEnabled() bool {
	return c.LLMProxy != nil && c.LLMProxy.AnthropicAPIKey != ""
}

// DefaultPath returns the path to the config file according to the XDG Base
// Directory Specification, honouring PINCHY_CONFIG if set.
func DefaultPath() (string, error) {
	if v := os.Getenv("PINCHY_CONFIG"); v != "" {
		return v, nil
	}

	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "pinchy", "config.yaml"), nil
}

// Load reads and parses the config file at path.  If path is empty, DefaultPath
// is used.  A missing file is not an error — Load returns an empty Config.
func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	warnIfLoosePermissions(path)

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config file %q: %w", path, err)
	}

	return &cfg, nil
}

// EnvFor returns the merged environment variable map for the named pinchy
// environment.  Global Env entries are applied first; per-environment Env
// entries override them.  The returned map is always non-nil and is a fresh
// copy — callers may modify it freely.
func (c *Config) EnvFor(name string) map[string]string {
	out := make(map[string]string, len(c.Env))
	for k, v := range c.Env {
		out[k] = v
	}
	if ec, ok := c.Environments[name]; ok {
		for k, v := range ec.Env {
			out[k] = v
		}
	}
	return out
}

// MountsFor returns the merged mount list for the named pinchy environment.
// Global Mounts are applied first; per-environment Mounts with the same
// Target replace the corresponding global entry (any new targets are
// appended). The returned slice is always a fresh copy — callers may
// modify it freely.
func (c *Config) MountsFor(name string) []Mount {
	// Build a target-keyed map from the global list, preserving order.
	order := make([]string, 0, len(c.Mounts))
	byTarget := make(map[string]Mount, len(c.Mounts))
	for _, m := range c.Mounts {
		if _, seen := byTarget[m.Target]; !seen {
			order = append(order, m.Target)
		}
		byTarget[m.Target] = m
	}

	// Merge per-environment entries on top.
	if ec, ok := c.Environments[name]; ok {
		for _, m := range ec.Mounts {
			if _, seen := byTarget[m.Target]; !seen {
				order = append(order, m.Target)
			}
			byTarget[m.Target] = m
		}
	}

	out := make([]Mount, 0, len(order))
	for _, t := range order {
		out = append(out, byTarget[t])
	}
	return out
}

// reservedMountTargets is the set of container paths that pinchy itself
// controls. User-defined mounts must not shadow them.
var reservedMountTargets = map[string]bool{
	"/data":          true,
	"/run/user/1000": true,
}

// validate checks that every key in every env map is a valid environment
// variable name (non-empty, no '=' character, no NUL byte), that any
// llm_proxy block has the required fields, and that all mount entries are
// well-formed.
func validate(cfg *Config) error {
	for k := range cfg.Env {
		if err := validateKey(k); err != nil {
			return err
		}
	}
	if err := validateMounts(cfg.Mounts, "mounts"); err != nil {
		return err
	}
	for envName, ec := range cfg.Environments {
		for k := range ec.Env {
			if err := validateKey(k); err != nil {
				return fmt.Errorf("environments.%s: %w", envName, err)
			}
		}
		if err := validateMounts(ec.Mounts, "environments."+envName+".mounts"); err != nil {
			return err
		}
	}
	if cfg.LLMProxy != nil && cfg.LLMProxy.AnthropicAPIKey == "" {
		return errors.New("llm_proxy.anthropic_api_key must not be empty when llm_proxy is set")
	}
	return nil
}

// validateMounts checks that a slice of Mount entries is well-formed.
// context is a human-readable path prefix used in error messages.
func validateMounts(mounts []Mount, context string) error {
	seen := make(map[string]bool, len(mounts))
	for i, m := range mounts {
		prefix := fmt.Sprintf("%s[%d]", context, i)
		if m.Source == "" {
			return fmt.Errorf("%s: source must not be empty", prefix)
		}
		if m.Target == "" {
			return fmt.Errorf("%s: target must not be empty", prefix)
		}
		if !filepath.IsAbs(m.Target) {
			return fmt.Errorf("%s: target %q must be an absolute path", prefix, m.Target)
		}
		if reservedMountTargets[m.Target] {
			return fmt.Errorf("%s: target %q is reserved by pinchy and cannot be overridden", prefix, m.Target)
		}
		switch m.Mode {
		case "", "ro", "rw":
			// valid
		default:
			return fmt.Errorf("%s: mode %q is invalid; accepted values are \"ro\" and \"rw\"", prefix, m.Mode)
		}
		if seen[m.Target] {
			return fmt.Errorf("%s: duplicate target %q in the same mount list", prefix, m.Target)
		}
		seen[m.Target] = true
	}
	return nil
}

// validateKey returns an error if name is not a valid environment variable key.
func validateKey(name string) error {
	if name == "" {
		return errors.New("environment variable name must not be empty")
	}
	if strings.ContainsAny(name, "=\x00") {
		return fmt.Errorf("environment variable name %q must not contain '=' or NUL", name)
	}
	return nil
}

// warnIfLoosePermissions prints a warning to stderr if the file at path is
// readable by group or other (i.e. mode bits g+r or o+r are set).  Stat
// errors are silently ignored — the warning is advisory only.
func warnIfLoosePermissions(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	if fi.Mode()&0o044 != 0 {
		fmt.Fprintf(os.Stderr, "warning: config file %q is readable by others; consider running: chmod 600 %q\n", path, path)
	}
}
