package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/codename"
	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/watch"
)

const defaultPollInterval = 60 * time.Second

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Poll for labelled issues and work them",
		Args:  cobra.NoArgs,
		RunE:  runWatch,
	}
}

func runWatch(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	root, err := repoRoot(ctx)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root, config.Overrides{})
	if err != nil {
		return err
	}
	verify, err := verifyCommands(cfg, "", false)
	if err != nil {
		return err
	}
	h, err := buildHarness(cfg.Harness.Default)
	if err != nil {
		return err
	}
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		return fmt.Errorf("parsing timeout %q: %w", cfg.Timeout, err)
	}

	owner, name, err := (&git.Repo{Root: root}).Origin(ctx)
	if err != nil {
		return fmt.Errorf("resolve origin: %w", err)
	}
	repo := owner + "/" + name

	store, err := job.Open(job.Path())
	if err != nil {
		return err
	}
	defer store.Close()

	w := &watch.Watcher{
		Repo:     repo,
		Trigger:  cfg.Label,
		Claim:    cfg.ClaimedLabel,
		Blocked:  cfg.BlockedLabel,
		Width:    cfg.Width,
		Interval: defaultPollInterval,
		GH:       &gh.Client{},
		Store:    store,
		RunJob: func(ctx context.Context, issue int) (string, error) {
			jobName := codename.For(repo, issue)
			f, err := openJobLog(owner, name, jobName)
			if err != nil {
				return "", err
			}
			defer f.Close()
			r, err := buildRunner(root, cfg, verify, h, timeout)
			if err != nil {
				return "", err
			}
			r.Codename = jobName
			r.Stderr = io.MultiWriter(os.Stderr, f)
			return r.Run(ctx, issue)
		},
		CleanJob: func(ctx context.Context, issue int) error {
			cache, err := os.UserCacheDir()
			if err != nil {
				return err
			}
			dir := filepath.Join(cache, "romp", owner+"-"+name, fmt.Sprintf("romp-%d", issue))
			r := &git.Repo{Root: root}
			if err := r.RemoveWorktree(ctx, dir); err != nil {
				return err
			}
			return r.DeleteBranch(ctx, fmt.Sprintf("romp-%d", issue))
		},
	}
	return w.Run(ctx)
}

// openJobLog creates or appends to the per-job log file for a codename.
func openJobLog(owner, name, codename string) (*os.File, error) {
	dir := job.LogsDir(owner, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating log dir: %w", err)
	}
	return os.OpenFile(filepath.Join(dir, codename+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}
