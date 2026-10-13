package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/BRO3886/romp/internal/daemon"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/progress"
	"github.com/BRO3886/romp/internal/protocol"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	return &cobra.Command{Use: "watch", Short: "Register this repository and view daemon jobs", Args: cobra.NoArgs, RunE: runWatch}
}

func runWatch(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	root, err := repoRoot(ctx)
	if err != nil {
		return err
	}
	c, err := daemonClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	var repo job.Repository
	if err := c.Call(ctx, "registry.watch", daemon.Params{Path: root}, &repo); err != nil {
		return err
	}
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		fmt.Fprintf(cmd.OutOrStdout(), "watching %s in rompd at %s\n", repo.Origin, repo.Path)
		return nil
	}
	events := make(chan progress.Event, 256)
	c.OnNotification = func(method string, raw json.RawMessage) {
		if method != "jobs.changed" {
			return
		}
		var value struct {
			Repo  string         `json:"repo"`
			Event progress.Event `json:"event"`
		}
		if json.Unmarshal(raw, &value) != nil || value.Repo != repo.Origin {
			return
		}
		select {
		case events <- value.Event:
		default:
			c.Close()
		}
	}
	if err := c.Call(ctx, "subscribe", nil, nil); err != nil {
		return err
	}
	var active []job.Job
	if err := c.Call(ctx, "jobs.list", daemon.Params{Repo: repo.Origin}, &active); err != nil {
		return err
	}
	for _, j := range active {
		select {
		case events <- progress.Event{Issue: j.Issue, Phase: progress.PhasePreparing, Detail: "running in rompd", BuilderHarness: j.BuilderHarness, ReviewerHarness: j.ReviewerHarness}:
		default:
			return fmt.Errorf("too many active job events; reconnect the dashboard")
		}
	}
	return runWatchTUI(ctx, &remoteWatch{c, events}, remoteHistory{}, repo.Origin, events)
}

type remoteWatch struct {
	client *protocol.Client
	events chan progress.Event
}

func (w *remoteWatch) Run(ctx context.Context) error {
	defer close(w.events)
	err := w.client.Listen(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

type remoteHistory struct{}

func (remoteHistory) History(ctx context.Context, repo string, limit int) ([]job.Outcome, error) {
	var out []job.Outcome
	err := daemonCall(ctx, "jobs.history", daemon.Params{Repo: repo, Limit: limit}, &out)
	return out, err
}

func newUnwatchCmd() *cobra.Command {
	return &cobra.Command{Use: "unwatch", Short: "Stop new claims for this repository", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		owner, name, err := currentRepo(cmd.Context())
		if err != nil {
			return err
		}
		return daemonCall(cmd.Context(), "registry.unwatch", daemon.Params{Repo: owner + "/" + name}, nil)
	}}
}
func newRebindCmd() *cobra.Command {
	return &cobra.Command{Use: "rebind", Short: "Bind a watched origin to this checkout", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := repoRoot(cmd.Context())
		if err != nil {
			return err
		}
		return daemonCall(cmd.Context(), "registry.rebind", daemon.Params{Path: root}, nil)
	}}
}
