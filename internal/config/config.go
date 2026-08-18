// Package config loads romp's per-repo configuration from TOML files.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the merged configuration for one repo. Every field's zero value
// means "use the built-in default", which makes layering additive: a later
// file overrides only the fields it actually sets.
type Config struct {
	Label        string  `toml:"label"`
	ClaimedLabel string  `toml:"claimed_label"`
	BlockedLabel string  `toml:"blocked_label"`
	Base         string  `toml:"base"`
	Width        int     `toml:"width"`
	Timeout      string  `toml:"timeout"`
	Verify       Verify  `toml:"verify"`
	Scope        Scope   `toml:"scope"`
	Harness      Harness `toml:"harness"`
	Prompt       Prompt  `toml:"prompt"`
}

// Verify holds the commands romp re-runs itself before opening a PR.
type Verify struct {
	Build string `toml:"build"`
	Test  string `toml:"test"`
	Lint  string `toml:"lint"`
}

// Scope holds path globs the agent must not touch (protected) or read (ignore).
type Scope struct {
	Protected []string `toml:"protected"`
	Ignore    []string `toml:"ignore"`
}

// Harness selects the coding-agent CLI and its options.
type Harness struct {
	Default  string `toml:"default"`
	Model    string `toml:"model"`
	MaxTurns int    `toml:"max_turns"`
}

// Prompt points at optional custom goal-contract files.
type Prompt struct {
	Template string `toml:"template"`
	Brief    string `toml:"brief"`
}

// Defaults returns the built-in configuration before any file or flag
// contributes a value.
func Defaults() Config {
	return Config{
		Label:        "romp",
		ClaimedLabel: "romp:claimed",
		BlockedLabel: "romp:blocked",
		Width:        3,
		Timeout:      "25m",
		Harness:      Harness{Default: "claude"},
	}
}

// Overrides carries command-line values that outrank every file.
type Overrides struct {
	Harness string
	Model   string
	Width   int
}

// Load merges, in order of increasing precedence, the built-in defaults, the
// user config, romp.toml, .romp/local.toml, and o. A missing file is fine; a
// malformed one is an error.
func Load(root string, o Overrides) (*Config, error) {
	cfg := Defaults()

	for _, f := range []string{userConfigPath(), filepath.Join(root, "romp.toml"), filepath.Join(root, ".romp", "local.toml")} {
		if f == "" {
			continue
		}
		if err := apply(&cfg, f); err != nil {
			return nil, err
		}
	}

	if o.Harness != "" {
		cfg.Harness.Default = o.Harness
	}
	if o.Model != "" {
		cfg.Harness.Model = o.Model
	}
	if o.Width != 0 {
		cfg.Width = o.Width
	}
	return &cfg, nil
}

func apply(dst *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	var src Config
	if err := toml.Unmarshal(data, &src); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	overlay(dst, &src)
	return nil
}

// overlay copies non-zero fields from src over dst. Zero means "not set", so
// dst keeps its current (lower-precedence) value for anything src leaves out.
func overlay(dst *Config, src *Config) {
	if src.Label != "" {
		dst.Label = src.Label
	}
	if src.ClaimedLabel != "" {
		dst.ClaimedLabel = src.ClaimedLabel
	}
	if src.BlockedLabel != "" {
		dst.BlockedLabel = src.BlockedLabel
	}
	if src.Base != "" {
		dst.Base = src.Base
	}
	if src.Width != 0 {
		dst.Width = src.Width
	}
	if src.Timeout != "" {
		dst.Timeout = src.Timeout
	}
	if src.Verify.Build != "" {
		dst.Verify.Build = src.Verify.Build
	}
	if src.Verify.Test != "" {
		dst.Verify.Test = src.Verify.Test
	}
	if src.Verify.Lint != "" {
		dst.Verify.Lint = src.Verify.Lint
	}
	if src.Scope.Protected != nil {
		dst.Scope.Protected = src.Scope.Protected
	}
	if src.Scope.Ignore != nil {
		dst.Scope.Ignore = src.Scope.Ignore
	}
	if src.Harness.Default != "" {
		dst.Harness.Default = src.Harness.Default
	}
	if src.Harness.Model != "" {
		dst.Harness.Model = src.Harness.Model
	}
	if src.Harness.MaxTurns != 0 {
		dst.Harness.MaxTurns = src.Harness.MaxTurns
	}
	if src.Prompt.Template != "" {
		dst.Prompt.Template = src.Prompt.Template
	}
	if src.Prompt.Brief != "" {
		dst.Prompt.Brief = src.Prompt.Brief
	}
}

func userConfigPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "romp", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "romp", "config.toml")
}
