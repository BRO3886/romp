// Package config loads romp's per-repo configuration from TOML files.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the merged configuration for one repo. Every field's zero value
// means "use the lower-precedence value", which makes layering additive: a
// later file overrides only the fields it actually sets.
type Config struct {
	Label                 string  `toml:"label"`
	ClaimedLabel          string  `toml:"claimed_label"`
	BlockedLabel          string  `toml:"blocked_label"`
	ChangesRequestedLabel string  `toml:"changes_requested_label"`
	Base                  string  `toml:"base"`
	Width                 int     `toml:"width"`
	Timeout               string  `toml:"timeout"`
	HistoryDays           int     `toml:"history_days"`
	Verify                Verify  `toml:"verify"`
	Scope                 Scope   `toml:"scope"`
	Harness               Harness `toml:"harness"`
	Review                Review  `toml:"review"`
	// HarnessEffortSource records the config file that supplied the effective
	// harness effort. It is empty for the built-in default and command-line
	// overrides.
	HarnessEffortSource string `toml:"-"`
	Prompt              Prompt `toml:"prompt"`
}

// Verify holds the commands romp re-runs itself before opening a PR.
type Verify struct {
	Commands []string `toml:"commands"`
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
	Effort   string `toml:"effort"`
	MaxTurns int    `toml:"max_turns"`
}

// Review configures the review gate independently from the builder.
type Review struct {
	Enabled bool   `toml:"enabled"`
	Model   string `toml:"model"`
	Harness string `toml:"harness"`
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
		Label:                 "romp",
		ClaimedLabel:          "romp:claimed",
		BlockedLabel:          "romp:blocked",
		ChangesRequestedLabel: "romp:changes-requested",
		Width:                 3,
		HistoryDays:           30,
		Harness:               Harness{Default: "codex", Effort: "high"},
		Review:                Review{Enabled: true},
	}
}

// ReviewModel returns the configured reviewer model or the builder model.
func (c Config) ReviewModel() string {
	if c.Review.Model != "" {
		return c.Review.Model
	}
	return c.Harness.Model
}

// ReviewHarness returns the configured reviewer harness or the builder harness.
func (c Config) ReviewHarness() string {
	if c.Review.Harness != "" {
		return c.Review.Harness
	}
	return c.Harness.Default
}

// Overrides carries command-line values that outrank every file.
type Overrides struct {
	Harness  string
	Model    string
	Effort   string
	Width    int
	NoReview bool
}

// Load merges, in order of increasing precedence, the built-in defaults, the
// user config, romp.toml, .romp/local.toml, and o. A missing file is fine; a
// malformed one is an error. After the merge it rejects an unknown harness
// name or an unsupported effort value for a harness with a stable effort set.
// OpenCode variants remain model-specific and pass through unchanged.
func Load(root string, o Overrides) (*Config, error) {
	cfg := Defaults()

	userCfg := userConfigPath()
	for _, f := range []string{userCfg, filepath.Join(root, "romp.toml"), filepath.Join(root, ".romp", "local.toml")} {
		if f == "" {
			continue
		}
		if err := apply(&cfg, f, f == userCfg); err != nil {
			return nil, err
		}
	}

	if o.Harness != "" {
		cfg.Harness.Default = o.Harness
	}
	if o.Model != "" {
		cfg.Harness.Model = o.Model
	}
	if o.Effort != "" {
		cfg.Harness.Effort = o.Effort
		cfg.HarnessEffortSource = ""
	}
	if o.Width != 0 {
		cfg.Width = o.Width
	}
	if o.NoReview {
		cfg.Review.Enabled = false
	}
	if err := validateHarness(cfg.Harness); err != nil {
		return nil, err
	}
	if err := validateHarnessName(cfg.ReviewHarness()); err != nil {
		return nil, fmt.Errorf("review.harness: %w", err)
	}
	return &cfg, nil
}

// apply reads one TOML file into src and overlays it on dst. global marks the
// user config file, which is the only source for machine-wide settings like
// HistoryDays.
func apply(dst *Config, path string, global bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	var src Config
	metadata, err := toml.Decode(string(data), &src)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, key := range metadata.Undecoded() {
		if len(key) > 0 && key[0] == "review" {
			return fmt.Errorf("parsing %s: unknown key %s", path, key.String())
		}
	}
	overlay(dst, &src, global, metadata)
	if src.Harness.Effort != "" {
		dst.HarnessEffortSource = path
	}
	return nil
}

// overlay copies non-zero fields from src over dst. A review table resets its
// model and harness to builder fallbacks unless it defines replacements.
// Review enabled uses field presence so omission retains the lower layer.
// Global-only fields are copied solely when src is the user config file.
func overlay(dst *Config, src *Config, global bool, metadata toml.MetaData) {
	if src.Label != "" {
		dst.Label = src.Label
	}
	if src.ClaimedLabel != "" {
		dst.ClaimedLabel = src.ClaimedLabel
	}
	if src.BlockedLabel != "" {
		dst.BlockedLabel = src.BlockedLabel
	}
	if src.ChangesRequestedLabel != "" {
		dst.ChangesRequestedLabel = src.ChangesRequestedLabel
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
	if global && src.HistoryDays != 0 {
		dst.HistoryDays = src.HistoryDays
	}
	if src.Verify.Commands != nil {
		dst.Verify.Commands = append([]string(nil), src.Verify.Commands...)
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
	if src.Harness.Effort != "" {
		dst.Harness.Effort = src.Harness.Effort
	}
	if src.Harness.MaxTurns != 0 {
		dst.Harness.MaxTurns = src.Harness.MaxTurns
	}
	if metadata.IsDefined("review", "enabled") {
		dst.Review.Enabled = src.Review.Enabled
	}
	if metadata.IsDefined("review") {
		dst.Review.Model = src.Review.Model
		dst.Review.Harness = src.Review.Harness
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
