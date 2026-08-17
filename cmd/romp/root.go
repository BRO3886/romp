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

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "romp",
		Short:         "Label an issue. Get a pull request.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRunCmd())
	return root
}

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run one issue now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			issue, err := cmd.Flags().GetInt("issue")
			if err != nil {
				return err
			}
			return runIssue(cmd.Context(), issue)
		},
	}
	cmd.Flags().IntP("issue", "i", 0, "issue number to run")
	cmd.MarkFlagRequired("issue")
	return cmd
}

func runIssue(ctx context.Context, issue int) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}

	r := &runner.Runner{
		Harness: harness.Claude{},
		Git:     &git.Repo{Root: root},
		GH:      &gh.Client{},
		Prompt: &prompt.Renderer{Template: prompt.Default()},
		// Temporary Go-specific default. Verify must become an explicit
		// per-repo contract (romp.toml [verify]); remove this fallback once
		// config lands and refuse to run without it.
		Verify: "go test ./... -count=1",
		Stderr: os.Stderr,
	}
	return r.Run(ctx, issue)
}
