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
	"github.com/BRO3886/romp/internal/harness"
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
	cfg, err := loadConfig(root, config.Overrides{}, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	verify, err := verifyCommands(cfg, nil, false)
	if err != nil {
		return err
	}
	h, err := buildHarness(cfg.Harness.Default)
	if err != nil {
		return err
	}
	var reviewer harness.Harness
	if cfg.Review.Enabled {
		reviewer, err = buildReviewHarness(cfg)
		if err != nil {
			return err
		}
	}
	timeout, err := parseTimeout(cfg.Timeout)
	if err != nil {
		return err
	}

	repository := &git.Repo{Root: root}
	owner, name, err := repository.Origin(ctx)
	if err != nil {
		return fmt.Errorf("resolve origin: %w", err)
	}
	repo := owner + "/" + name
	factory := runnerFactory{
		root:       root,
		config:     cfg,
		verify:     verify,
		harness:    h,
		reviewer:   reviewer,
		timeout:    timeout,
		repository: repository,
	}

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
			r, err := factory.build()
			if err != nil {
				return "", err
			}
			r.Codename = jobName
			r.Sessions = store
			r.Stderr = io.MultiWriter(watch.NewColorWriter(os.Stderr, jobName), f)
			return r.Run(ctx, issue)
		},
		Stderr: watch.NewColorWriter(os.Stderr, ""),
		CleanJob: func(ctx context.Context, issue int) error {
			cache, err := os.UserCacheDir()
			if err != nil {
				return err
			}
			dir := filepath.Join(cache, "romp", owner+"-"+name, fmt.Sprintf("romp-%d", issue))
			if err := repository.RemoveWorktree(ctx, dir); err != nil {
				return err
			}
			return repository.DeleteBranch(ctx, fmt.Sprintf("romp-%d", issue))
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
