package config

import (
	"fmt"
	"strings"
)

// effortsByHarness is the legal [harness].effort set for each adapter, taken
// from the live CLIs: `claude --help` (low, medium, high, xhigh, max) and
// Codex's model_reasoning_effort (those plus none, minimal, ultra). OpenCode
// passes effort as a model-specific `--variant`, so Romp does not validate its
// values. Shared names mean the same thing and pass through unchanged.
var effortsByHarness = map[string][]string{
	"claude":   {"low", "medium", "high", "xhigh", "max"},
	"codex":    {"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"},
	"opencode": nil,
}

func validateHarness(h Harness) error {
	allowed, ok := effortsByHarness[h.Default]
	if !ok {
		return fmt.Errorf("unknown harness %q (want claude, codex, or opencode)", h.Default)
	}
	// OpenCode accepts model-specific variant names, so validation belongs to
	// OpenCode rather than this shared configuration layer.
	if h.Default == "opencode" {
		return nil
	}
	if h.Effort == "" {
		return nil
	}
	if !contains(allowed, h.Effort) {
		return fmt.Errorf("harness.effort %q is not valid for %s (want %s)", h.Effort, h.Default, strings.Join(allowed, ", "))
	}
	return nil
}

func contains(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}
