package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"strconv"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/codename"
	"github.com/BRO3886/romp/internal/daemon"
	"github.com/BRO3886/romp/internal/job"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <codename|issue>",
		Short: "Show a job's log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd.Context(), args[0], follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new lines")
	return cmd
}

func runLogs(ctx context.Context, arg string, follow bool) error {
	owner, name, err := currentRepo(ctx)
	if err != nil {
		return err
	}
	c, err := daemonClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	p := daemon.Params{Repo: owner + "/" + name, Name: logCodename(owner, name, arg)}
	var pending []string
	c.OnNotification = func(method string, raw json.RawMessage) {
		if method == "logs.record" {
			var record struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(raw, &record) == nil {
				pending = append(pending, record.Text)
			}
		}
	}
	method := "logs.snapshot"
	if follow {
		method = "logs.subscribe"
	}
	for {
		var snapshot daemon.LogSnapshot
		if err := c.Call(ctx, method, p, &snapshot); err != nil {
			return err
		}
		fmt.Print(snapshot.Text)
		p.Offset = snapshot.Offset
		if snapshot.EOF {
			break
		}
	}
	for _, text := range pending {
		fmt.Print(text)
	}
	if !follow {
		return nil
	}
	c.OnNotification = func(method string, raw json.RawMessage) {
		if method == "logs.record" {
			var record struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(raw, &record) == nil {
				fmt.Print(record.Text)
			}
		}
	}
	return c.Listen(ctx)
}

// jobLogPath is the per-repo log file for a job, addressed by codename or
// issue number. The directory is owner-repo, never owner-codename.
func jobLogPath(owner, repo, arg string) string {
	return filepath.Join(job.LogsDir(owner, repo), logCodename(owner, repo, arg)+".log")
}

func logCodename(owner, repo, arg string) string {
	n, err := strconv.Atoi(arg)
	if err != nil || n <= 0 {
		return arg
	}
	return codename.For(owner+"/"+repo, n)
}
