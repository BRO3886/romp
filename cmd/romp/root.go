package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/prompt"
	"github.com/BRO3886/romp/internal/runner"
)

// Temporary Go-specific default. Verify must become an explicit per-repo
// contract (romp.toml [verify]); remove this fallback once config lands and
// refuse to run without it.
const defaultVerify = "go test ./... -count=1"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "romp",
		Short:         "Label an issue. Get a pull request.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRunCmd(execRun))
	return root
}

// runFunc is the seam between flag parsing and the job itself, so tests can
// inspect the assembled Runner without touching git, gh, or the harness.
type runFunc func(ctx context.Context, r *runner.Runner, issue int) error

func newRunCmd(run runFunc) *cobra.Command {
	var (
		issue  int
		verify string
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run one issue now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := newRunner(verify)
			if err != nil {
				return err
			}
			return run(cmd.Context(), r, issue)
		},
	}
	cmd.Flags().IntVarP(&issue, "issue", "i", 0, "issue number to run")
	cmd.Flags().StringVar(&verify, "verify", defaultVerify, "command that must pass in the worktree before a PR is opened")
	cmd.MarkFlagRequired("issue")
	return cmd
}

func newRunner(verify string) (*runner.Runner, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}

	return &runner.Runner{
		Harness: harness.Claude{},
		Git:     &git.Repo{Root: root},
		GH:      &gh.Client{},
		Prompt:  &prompt.Renderer{Template: prompt.Default()},
		Verify:  verify,
		Stderr:  os.Stderr,
	}, nil
}

func execRun(ctx context.Context, r *runner.Runner, issue int) error {
	return r.Run(ctx, issue)
}
