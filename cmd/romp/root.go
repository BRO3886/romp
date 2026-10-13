package main

import (
	"context"
	"fmt"
	"os"

	"github.com/BRO3886/romp/internal/git"
	"github.com/spf13/cobra"
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
	root.AddCommand(newRemoteRunCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newUnwatchCmd())
	root.AddCommand(newRebindCmd())
	root.AddCommand(newDaemonCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newGcCmd())
	root.AddCommand(newHistoryCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newCancelCmd())
	root.AddCommand(newLogsCmd())
	root.AddCommand(defaultSkillsCmd())
	return root
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
