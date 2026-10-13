package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/daemon"
)

func newGcCmd() *cobra.Command {
	var apply bool
	var historyDays int
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Remove stale worktrees and old job history",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGc(cmd, apply, historyDays)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "delete worktrees and history instead of listing them")
	cmd.Flags().IntVar(&historyDays, "history-days", 0, "delete outcomes older than N days (0 disables; default from user config)")
	return cmd
}

func runGc(cmd *cobra.Command, apply bool, historyDays int) error {
	root, err := repoRoot(cmd.Context())
	if err != nil {
		return err
	}
	days := -1
	if cmd.Flags().Changed("history-days") {
		days = historyDays
		if days < 0 {
			return fmt.Errorf("history-days must be non-negative")
		}
	}
	var result daemon.GCResult
	if err := daemonCall(cmd.Context(), "gc", daemon.Params{Path: root, Days: days, Apply: apply}, &result); err != nil {
		return err
	}
	verb := "would remove"
	if apply {
		verb = "removed"
	}
	for _, path := range result.Worktrees {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", verb, path)
	}
	if result.Outcomes > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", verb, outcomeWord(result.Outcomes))
	}
	if len(result.Worktrees) == 0 && result.Outcomes == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "nothing to clean")
	}
	return nil
}

// staleWorktrees returns the romp-N worktree names whose job is not in-flight
// (no active row), so they are safe to reclaim.
func staleWorktrees(names []string, active map[int]bool) []string {
	var out []string
	for _, n := range names {
		if !strings.HasPrefix(n, "romp-") {
			continue
		}
		num, err := strconv.Atoi(strings.TrimPrefix(n, "romp-"))
		if err != nil {
			continue
		}
		if active[num] {
			continue
		}
		out = append(out, n)
	}
	return out
}

// outcomeWord renders n with the right noun form for the gc messages.
func outcomeWord(n int) string {
	if n == 1 {
		return "1 outcome"
	}
	return fmt.Sprintf("%d outcomes", n)
}
