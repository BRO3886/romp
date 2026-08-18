package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

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

	store, err := job.Open(job.Path(owner, name))
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
		RunJob: func(ctx context.Context, issue int) error {
			return buildRunner(root, cfg, verify, h, timeout).Run(ctx, issue)
		},
	}
	return w.Run(ctx)
}
