// Package execution assembles the repository workflow owned by rompd.
package execution

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/prompt"
	"github.com/BRO3886/romp/internal/runner"
)

// Build validates configuration and assembles a runner without starting work.
func Build(ctx context.Context, root string, o config.Overrides, verifyFlags []string, verifySet bool) (*runner.Runner, error) {
	cfg, err := config.Load(root, o)
	if err != nil {
		return nil, err
	}

	verify, err := VerifyCommands(cfg, verifyFlags, verifySet)
	if err != nil {
		return nil, err
	}

	h, err := buildHarness(cfg.Harness.Default)
	if err != nil {
		return nil, err
	}
	var reviewer harness.Harness
	if cfg.Review.Enabled {
		reviewer, err = BuildReviewHarness(cfg)
		if err != nil {
			return nil, err
		}
	}

	timeout, err := parseTimeout(cfg.Timeout)
	if err != nil {
		return nil, err
	}

	factory := runnerFactory{
		root:       root,
		config:     cfg,
		verify:     verify,
		harness:    h,
		reviewer:   reviewer,
		timeout:    timeout,
		repository: &git.Repo{Root: root},
	}
	return factory.build()
}

func parseTimeout(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parsing timeout %q: %w", value, err)
	}
	return timeout, nil
}

type runnerFactory struct {
	root       string
	config     *config.Config
	verify     []string
	harness    harness.Harness
	reviewer   harness.Harness
	timeout    time.Duration
	repository runner.GitOps
}

func (f runnerFactory) build() (*runner.Runner, error) {
	templateText, err := loadTemplate(f.root, f.config.Prompt.Template)
	if err != nil {
		return nil, err
	}
	brief, err := resolveBrief(f.root, f.config.Prompt.Brief)
	if err != nil {
		return nil, err
	}
	return &runner.Runner{
		Harness:               f.harness,
		ReviewHarness:         f.reviewer,
		Git:                   f.repository,
		GH:                    &gh.Client{},
		Prompt:                &prompt.Renderer{Template: templateText},
		Verify:                f.verify,
		Model:                 f.config.Harness.Model,
		ReviewModel:           f.config.ReviewModel(),
		ReviewEnabled:         f.config.Review.Enabled,
		MaxFixRounds:          f.config.Review.MaxFixRounds,
		Effort:                f.config.Harness.Effort,
		MaxTurns:              f.config.Harness.MaxTurns,
		Base:                  f.config.Base,
		Timeout:               f.timeout,
		Protected:             f.config.Scope.Protected,
		Ignore:                f.config.Scope.Ignore,
		Brief:                 brief,
		TriggerLabel:          f.config.Label,
		BlockedLabel:          f.config.BlockedLabel,
		ChangesRequestedLabel: f.config.ChangesRequestedLabel,
		Stderr:                os.Stderr,
	}, nil
}

// loadTemplate returns the goal-contract template text: the configured file
// when set, otherwise the built-in default. A configured file that cannot be
// read is an error rather than a silent fallback.
func loadTemplate(root, path string) (string, error) {
	if path == "" {
		return prompt.Default(), nil
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return "", fmt.Errorf("reading prompt template %s: %w", path, err)
	}
	return string(data), nil
}

// resolveBrief validates that a configured brief file exists and returns its
// path, which the prompt passes to the agent as a READ FIRST pointer (the
// agent reads the file itself in the worktree).
func resolveBrief(root, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if _, err := os.Stat(filepath.Join(root, path)); err != nil {
		return "", fmt.Errorf("prompt brief %s: %w", path, err)
	}
	return path, nil
}

// verifyCommands resolves the verify list: the --verify flag when it was
// passed, otherwise the non-empty [verify] commands from config. It refuses
// to guess (see ADR 0001).
func VerifyCommands(cfg *config.Config, flags []string, flagSet bool) ([]string, error) {
	if flagSet {
		return append([]string(nil), flags...), nil
	}
	if len(cfg.Verify.Commands) == 0 {
		return nil, fmt.Errorf("no verify command configured: run `romp init` or pass --verify")
	}
	return append([]string(nil), cfg.Verify.Commands...), nil
}

func buildHarness(name string) (harness.Harness, error) {
	switch name {
	case "claude":
		return harness.Claude{}, nil
	case "codex":
		return harness.Codex{}, nil
	case "opencode":
		return harness.OpenCode{}, nil
	default:
		return nil, fmt.Errorf("unknown harness %q (want claude, codex, or opencode)", name)
	}
}

func BuildReviewHarness(cfg *config.Config) (harness.Harness, error) {
	return buildHarness(cfg.ReviewHarness())
}
