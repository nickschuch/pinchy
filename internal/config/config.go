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

// EnvConfig holds per-environment configuration overrides.
type EnvConfig struct {
	// Env is a map of environment variable names to values injected into the
	// agent container for this specific environment. These values override the
	// top-level Env entries for the same keys.
	Env map[string]string `yaml:"env"`
}

// Config is the top-level structure for the pinchy configuration file.
type Config struct {
	// Env is a map of environment variable names to values that are injected
	// into the agent container of every environment.
	Env map[string]string `yaml:"env"`

	// Environments holds per-environment overrides keyed by environment name.
	// Values here are merged on top of the global Env entries, with
	// per-environment values winning.
	Environments map[string]EnvConfig `yaml:"environments"`
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

// validate checks that every key in every env map is a valid environment
// variable name (non-empty, no '=' character, no NUL byte).
func validate(cfg *Config) error {
	for k := range cfg.Env {
		if err := validateKey(k); err != nil {
			return err
		}
	}
	for envName, ec := range cfg.Environments {
		for k := range ec.Env {
			if err := validateKey(k); err != nil {
				return fmt.Errorf("environments.%s: %w", envName, err)
			}
		}
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
