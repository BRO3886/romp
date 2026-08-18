package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/job"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <codename>",
		Short: "Show a job's log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd.Context(), args[0], follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new lines")
	return cmd
}

func runLogs(ctx context.Context, codename string, follow bool) error {
	owner, name, err := currentRepo(ctx)
	if err != nil {
		return err
	}
	path := filepath.Join(job.LogsDir(owner, name), codename+".log")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no log for %s (is the job running?)", codename)
	}
	if !follow {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(data)
		return err
	}
	cmd := exec.CommandContext(ctx, "tail", "-f", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
