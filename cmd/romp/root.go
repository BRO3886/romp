package main

import (
	"context"
	"fmt"
	"os"
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
	}
	root.AddCommand(newRunCmd(newRunner, execRun))
	root.AddCommand(newInitCmd())
	root.AddCommand(newWatchCmd())
	return root
}

// runFactory assembles a Runner from the merged config plus the --verify
// flag. It is the seam between flag parsing and the git/gh/harness wiring so
// tests can inject a fake.
type runFactory func(ctx context.Context, o config.Overrides, verifyFlag string, verifySet bool) (*runner.Runner, error)

// runFunc is the seam between the assembled Runner and the job itself, so
// tests can inspect the Runner without running the pipeline.
type runFunc func(ctx context.Context, r *runner.Runner, issue int) error

func newRunCmd(factory runFactory, run runFunc) *cobra.Command {
	var (
		issue       int
		verify      string
		harnessName string
		model       string
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
				Width:   width,
			}, verify, cmd.Flags().Changed("verify"))
			if err != nil {
				return err
			}
			return run(cmd.Context(), r, issue)
		},
	}
	cmd.Flags().IntVarP(&issue, "issue", "i", 0, "issue number to run")
	cmd.Flags().StringVar(&verify, "verify", "", "command that must pass in the worktree before a PR opens (overrides config)")
	cmd.Flags().StringVar(&harnessName, "harness", "", "harness to use (claude or codex)")
	cmd.Flags().StringVar(&model, "model", "", "model for the harness")
	cmd.Flags().IntVar(&width, "width", 0, "concurrent jobs (ignored by run)")
	cmd.MarkFlagRequired("issue")
	return cmd
}

func newRunner(ctx context.Context, o config.Overrides, verifyFlag string, verifySet bool) (*runner.Runner, error) {
	root, err := repoRoot(ctx)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(root, o)
	if err != nil {
		return nil, err
	}

	verify, err := verifyCommands(cfg, verifyFlag, verifySet)
	if err != nil {
		return nil, err
	}

	h, err := buildHarness(cfg.Harness.Default)
	if err != nil {
		return nil, err
	}

	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("parsing timeout %q: %w", cfg.Timeout, err)
	}

	return buildRunner(root, cfg, verify, h, timeout), nil
}

// buildRunner assembles a Runner from already-resolved config and options so
// both the run and watch commands share one construction path.
func buildRunner(root string, cfg *config.Config, verify []string, h harness.Harness, timeout time.Duration) *runner.Runner {
	return &runner.Runner{
		Harness:      h,
		Git:          &git.Repo{Root: root},
		GH:           &gh.Client{},
		Prompt:       &prompt.Renderer{Template: prompt.Default()},
		Verify:       verify,
		Model:        cfg.Harness.Model,
		Effort:       cfg.Harness.Effort,
		Base:         cfg.Base,
		Timeout:      timeout,
		Protected:    cfg.Scope.Protected,
		TriggerLabel: cfg.Label,
		BlockedLabel: cfg.BlockedLabel,
		Stderr:       os.Stderr,
	}
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
func verifyCommands(cfg *config.Config, flag string, flagSet bool) ([]string, error) {
	if flagSet {
		return []string{flag}, nil
	}
	var out []string
	for _, c := range []string{cfg.Verify.Build, cfg.Verify.Test, cfg.Verify.Lint} {
		if c != "" {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no verify command configured: run `romp init` or pass --verify")
	}
	return out, nil
}

func buildHarness(name string) (harness.Harness, error) {
	switch name {
	case "claude":
		return harness.Claude{}, nil
	case "codex":
		return nil, fmt.Errorf("harness codex is not implemented yet")
	default:
		return nil, fmt.Errorf("unknown harness %q (want claude or codex)", name)
	}
}

func execRun(ctx context.Context, r *runner.Runner, issue int) error {
	return r.Run(ctx, issue)
}
