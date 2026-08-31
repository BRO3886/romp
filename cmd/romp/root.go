package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/prompt"
	"github.com/BRO3886/romp/internal/runner"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "romp",
		Short:         "Label an issue. Get a pull request.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.SetVersionTemplate(fmt.Sprintf("romp %s (commit %s, built %s)\n", version, commit, buildTime))
	root.AddCommand(newRunCmd(newRunner, execRun))
	root.AddCommand(newInitCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newGcCmd())
	root.AddCommand(newHistoryCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newCancelCmd())
	root.AddCommand(newLogsCmd())
	return root
}

// runFactory assembles a Runner from the merged config plus the --verify
// flag. It is the seam between flag parsing and the git/gh/harness wiring so
// tests can inject a fake.
type runFactory func(ctx context.Context, o config.Overrides, verifyFlags []string, verifySet bool) (*runner.Runner, error)

// runFunc is the seam between the assembled Runner and the job itself, so
// tests can inspect the Runner without running the pipeline.
type runFunc func(ctx context.Context, r *runner.Runner, issue int) error

func newRunCmd(factory runFactory, run runFunc) *cobra.Command {
	var (
		issue       int
		verify      []string
		harnessName string
		model       string
		effort      string
		width       int
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run one issue now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := factory(cmd.Context(), config.Overrides{
				Harness: harnessName,
				Model:   model,
				Effort:  effort,
				Width:   width,
			}, verify, cmd.Flags().Changed("verify"))
			if err != nil {
				return err
			}
			return run(cmd.Context(), r, issue)
		},
	}
	cmd.Flags().IntVarP(&issue, "issue", "i", 0, "issue number to run")
	cmd.Flags().StringArrayVar(&verify, "verify", nil, "command that must pass in the worktree before a PR opens (repeatable; overrides config)")
	cmd.Flags().StringVar(&harnessName, "harness", "", "harness to use (claude, codex, or opencode)")
	cmd.Flags().StringVar(&model, "model", "", "model for the harness")
	cmd.Flags().StringVar(&effort, "effort", "", "reasoning effort for the harness")
	cmd.Flags().IntVar(&width, "width", 0, "concurrent jobs (ignored by run)")
	cmd.MarkFlagRequired("issue")
	return cmd
}

func newRunner(ctx context.Context, o config.Overrides, verifyFlags []string, verifySet bool) (*runner.Runner, error) {
	root, err := repoRoot(ctx)
	if err != nil {
		return nil, err
	}

	cfg, err := loadConfig(root, o, os.Stderr)
	if err != nil {
		return nil, err
	}

	verify, err := verifyCommands(cfg, verifyFlags, verifySet)
	if err != nil {
		return nil, err
	}

	h, err := buildHarness(cfg.Harness.Default)
	if err != nil {
		return nil, err
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
		Harness:      f.harness,
		Git:          f.repository,
		GH:           &gh.Client{},
		Prompt:       &prompt.Renderer{Template: templateText},
		Verify:       f.verify,
		Model:        f.config.Harness.Model,
		Effort:       f.config.Harness.Effort,
		MaxTurns:     f.config.Harness.MaxTurns,
		Base:         f.config.Base,
		Timeout:      f.timeout,
		Protected:    f.config.Scope.Protected,
		Ignore:       f.config.Scope.Ignore,
		Brief:        brief,
		TriggerLabel: f.config.Label,
		BlockedLabel: f.config.BlockedLabel,
		Stderr:       os.Stderr,
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

func repoRoot(ctx context.Context) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	root, err := git.FindRoot(ctx, cwd)
	if err != nil {
		return "", fmt.Errorf("find git root: %w", err)
	}
	return root, nil
}

// verifyCommands resolves the verify list: the --verify flag when it was
// passed, otherwise the non-empty [verify] commands from config. It refuses
// to guess (see ADR 0001).
func verifyCommands(cfg *config.Config, flags []string, flagSet bool) ([]string, error) {
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

func execRun(ctx context.Context, r *runner.Runner, issue int) error {
	_, err := r.Run(ctx, issue)
	return err
}
