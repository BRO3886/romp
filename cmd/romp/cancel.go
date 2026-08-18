package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/watch"
)

func newCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <issue>",
		Short: "Cancel the running job for an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("issue must be a number: %w", err)
			}
			return runCancel(cmd.Context(), n)
		},
	}
}

func runCancel(ctx context.Context, issue int) error {
	owner, name, err := currentRepo(ctx)
	if err != nil {
		return err
	}
	if err := watch.CancelJob(owner+"/"+name, issue); err != nil {
		return err
	}
	fmt.Printf("cancelled #%d\n", issue)
	return nil
}
