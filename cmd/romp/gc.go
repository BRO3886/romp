package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/job"
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
	ctx := cmd.Context()
	root, err := repoRoot(ctx)
	if err != nil {
		return err
	}
	owner, name, err := (&git.Repo{Root: root}).Origin(ctx)
	if err != nil {
		return fmt.Errorf("resolve origin: %w", err)
	}

	cfg, err := config.Load(root, config.Overrides{})
	if err != nil {
		return err
	}
	days := cfg.HistoryDays
	if cmd.Flags().Changed("history-days") {
		days = historyDays
	}

	store, err := job.Open(job.Path())
	if err != nil {
		return err
	}
	defer store.Close()

	jobs, err := store.List(ctx, owner+"/"+name)
	if err != nil {
		return err
	}
	active := make(map[int]bool)
	for _, j := range jobs {
		active[j.Issue] = true
	}

	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	wtDir := filepath.Join(cacheRoot, "romp", owner+"-"+name)

	var names []string
	entries, err := os.ReadDir(wtDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		for _, e := range entries {
			names = append(names, e.Name())
		}
	}

	any := false
	g := &git.Repo{Root: root}
	for _, n := range staleWorktrees(names, active) {
		any = true
		dir := filepath.Join(wtDir, n)
		if apply {
			if err := g.RemoveWorktree(ctx, dir); err != nil {
				return err
			}
			fmt.Printf("removed %s\n", n)
		} else {
			fmt.Printf("would remove %s (pass --apply to delete)\n", n)
		}
	}

	if days > 0 {
		cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339Nano)
		n, err := store.CountBefore(ctx, cutoff)
		if err != nil {
			return err
		}
		if n > 0 {
			any = true
			if apply {
				if _, err := store.Prune(ctx, cutoff); err != nil {
					return err
				}
				fmt.Printf("pruned %s older than %dd\n", outcomeWord(n), days)
			} else {
				fmt.Printf("would prune %s older than %dd (pass --apply to delete)\n", outcomeWord(n), days)
			}
		}
	}

	if !any {
		fmt.Println("nothing to clean")
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
