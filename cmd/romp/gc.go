package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/git"
)

func newGcCmd() *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Remove stale worktrees from finished jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGc(cmd.Context(), apply)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "delete worktrees instead of listing them")
	return cmd
}

func runGc(ctx context.Context, apply bool) error {
	root, err := repoRoot(ctx)
	if err != nil {
		return err
	}
	owner, name, jobs, err := loadJobs(ctx)
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
	entries, err := os.ReadDir(wtDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("nothing to clean")
			return nil
		}
		return err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	stale := staleWorktrees(names, active)
	if len(stale) == 0 {
		fmt.Println("nothing to clean")
		return nil
	}

	g := &git.Repo{Root: root}
	for _, n := range stale {
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
