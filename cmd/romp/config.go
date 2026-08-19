package main

import (
	"fmt"
	"io"

	"github.com/BRO3886/romp/internal/config"
)

func loadConfig(root string, overrides config.Overrides, warnings io.Writer) (*config.Config, error) {
	cfg, err := config.Load(root, overrides)
	if err != nil {
		return nil, err
	}
	warnOpenCodeVariant(warnings, cfg)
	return cfg, nil
}

func warnOpenCodeVariant(out io.Writer, cfg *config.Config) {
	if cfg.Harness.Default != "opencode" || cfg.HarnessEffortSource == "" {
		return
	}
	model := cfg.Harness.Model
	if model == "" {
		model = "the selected model"
	}
	fmt.Fprintf(out, "warning: OpenCode variant %q may not be supported by %s; variants are model-specific (configured in %s)\n", cfg.Harness.Effort, model, cfg.HarnessEffortSource)
}
