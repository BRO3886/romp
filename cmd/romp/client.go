package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/BRO3886/romp/internal/codename"
	"github.com/BRO3886/romp/internal/daemon"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/protocol"
	"github.com/BRO3886/romp/internal/runner"
	"github.com/spf13/cobra"
)

func daemonClient(ctx context.Context) (*protocol.Client, error) {
	initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return protocol.Dial(initCtx, protocol.SocketPath(), "romp", version)
}
func daemonCall(ctx context.Context, method string, params, result any) error {
	c, err := daemonClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	callCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	return c.Call(callCtx, method, params, result)
}

func newRemoteRunCmd() *cobra.Command {
	var p daemon.Params
	cmd := &cobra.Command{Use: "run", Short: "Run one issue through rompd", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := repoRoot(cmd.Context())
		if err != nil {
			return err
		}
		p.Path = root
		p.VerifySet = cmd.Flags().Changed("verify")
		return runRemote(cmd, p)
	}}
	cmd.Flags().IntVarP(&p.Issue, "issue", "i", 0, "issue number to run")
	cmd.Flags().StringArrayVar(&p.Verify, "verify", nil, "ordered verification commands (repeatable)")
	cmd.Flags().StringVar(&p.Overrides.Harness, "harness", "", "claude, codex, or opencode")
	cmd.Flags().StringVar(&p.Overrides.Model, "model", "", "model for the harness")
	cmd.Flags().StringVar(&p.Overrides.Effort, "effort", "", "reasoning effort")
	cmd.Flags().IntVar(&p.Overrides.Width, "width", 0, "ignored by foreground run")
	cmd.Flags().BoolVar(&p.Overrides.NoReview, "no-review", false, "skip the review gate")
	cmd.MarkFlagRequired("issue")
	return cmd
}

func runRemote(cmd *cobra.Command, p daemon.Params) error {
	ctx := cmd.Context()
	owner, name, err := currentRepo(ctx)
	if err != nil {
		return err
	}
	p.Repo = owner + "/" + name
	c, err := daemonClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var finished *job.Outcome
	var logs []string
	streaming := false
	c.OnNotification = func(method string, raw json.RawMessage) {
		if method == "logs.record" {
			var r struct {
				Repo string `json:"repo"`
				Text string `json:"text"`
			}
			if json.Unmarshal(raw, &r) == nil && r.Repo == p.Repo {
				if streaming {
					fmt.Fprint(cmd.ErrOrStderr(), r.Text)
				} else {
					logs = append(logs, r.Text)
				}
			}
		}
		if method == "jobs.finished" {
			var o job.Outcome
			if json.Unmarshal(raw, &o) == nil && o.Repo == p.Repo && o.Issue == p.Issue {
				finished = &o
				cancel()
			}
		}
	}
	if err := c.Call(ctx, "subscribe", nil, nil); err != nil {
		return err
	}
	var oldLog daemon.LogSnapshot
	logParams := daemon.Params{Repo: p.Repo, Name: codename.For(p.Repo, p.Issue)}
	for {
		if err := c.Call(ctx, "logs.subscribe", logParams, &oldLog); err != nil {
			return err
		}
		logParams.Offset = oldLog.Offset
		if oldLog.EOF {
			break
		}
	}
	if err := c.Call(ctx, "jobs.run", p, nil); err != nil {
		return err
	}
	for _, text := range logs {
		fmt.Fprint(cmd.ErrOrStderr(), text)
	}
	streaming = true
	err = c.Listen(waitCtx)
	if finished != nil {
		if finished.Outcome == "done" {
			fmt.Fprintln(cmd.OutOrStdout(), finished.PRURL)
			return nil
		}
		if finished.Outcome == "blocked" {
			return fmt.Errorf("%w: %s", runner.ErrBlocked, finished.Detail)
		}
		return fmt.Errorf("%s: %s", finished.Outcome, finished.Detail)
	}
	if errors.Is(err, context.Canceled) {
		cancelCtx, stop := context.WithTimeout(context.Background(), 35*time.Second)
		defer stop()
		return errors.Join(err, daemonCall(cancelCtx, "jobs.cancel", p, nil))
	}
	return err
}
