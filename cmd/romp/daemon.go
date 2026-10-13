package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/BRO3886/romp/internal/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "daemon", Short: "Install and manage the machine-wide daemon"}
	var binary string
	var dryRun bool
	install := &cobra.Command{Use: "install", Short: "Resolve tools and stage the canonical daemon", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		if binary == "" {
			self, err := os.Executable()
			if err != nil {
				return err
			}
			binary = filepath.Join(filepath.Dir(self), "rompd")
			if _, err := os.Stat(binary); err != nil {
				binary, err = exec.LookPath("rompd")
				if err != nil {
					return fmt.Errorf("rompd binary not found; build both binaries or pass --binary")
				}
			}
		}
		binary, err = filepath.Abs(binary)
		if err != nil {
			return err
		}
		if dryRun {
			env, err := daemon.Preflight(cmd.Context(), root, binary)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(env)
		}
		paths, err := daemon.Install(cmd.Context(), root, binary)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "installed %s\nRun `romp daemon start` to load the agent\n", paths.Binary)
		return nil
	}}
	install.Flags().StringVar(&binary, "binary", "", "path to the distributed rompd artifact")
	install.Flags().BoolVar(&dryRun, "dry-run", false, "resolve real configuration and tools without installing")
	cmd.AddCommand(install)
	for _, action := range []string{"start", "stop", "restart", "status", "version"} {
		cmd.AddCommand(&cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			switch action {
			case "start":
				return startDaemon(ctx)
			case "stop":
				return stopDaemon(ctx)
			case "restart":
				c, err := daemonClient(ctx)
				if err != nil {
					return err
				}
				defer c.Close()
				if err := c.Call(ctx, "daemon.prepareRestart", nil, nil); err != nil {
					return err
				}
				if err := daemon.Launch(ctx, "bootout"); err != nil {
					return err
				}
				return startDaemon(ctx)
			default:
				var result json.RawMessage
				method := "daemon.health"
				if action == "version" {
					method = "daemon.version"
				}
				if err := daemonCall(ctx, method, nil, &result); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(result))
				return nil
			}
		}})
	}
	return cmd
}

func startDaemon(ctx context.Context) error {
	if err := daemon.Launch(ctx, "print"); err != nil {
		if err := daemon.Launch(ctx, "bootstrap"); err != nil {
			return err
		}
	}
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		if err := daemonCall(ctx, "daemon.health", nil, nil); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("launchd loaded but rompd is not ready; inspect daemon.log")
		case <-timer.C:
		}
	}
}

func stopDaemon(ctx context.Context) error {
	c, err := daemonClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Call(ctx, "daemon.prepareShutdown", nil, nil); err != nil {
		return err
	}
	return daemon.Launch(ctx, "bootout")
}
