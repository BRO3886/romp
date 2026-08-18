package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/codename"
	"github.com/BRO3886/romp/internal/job"
)

func newHistoryCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show recently finished jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHistory(cmd, all)
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "list jobs across every repo on this machine")
	return cmd
}

func runHistory(cmd *cobra.Command, all bool) error {
	store, err := job.Open(job.Path())
	if err != nil {
		return err
	}
	defer store.Close()

	repo := ""
	if !all {
		owner, name, err := currentRepo(cmd.Context())
		if err != nil {
			return err
		}
		repo = owner + "/" + name
	}
	outcomes, err := store.History(cmd.Context(), repo, 20)
	if err != nil {
		return err
	}
	if len(outcomes) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no jobs finished yet")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	if all {
		fmt.Fprintln(w, "CODENAME\tREPO\tISSUE\tOUTCOME\tPR\tFINISHED")
	} else {
		fmt.Fprintln(w, "CODENAME\tISSUE\tOUTCOME\tPR\tFINISHED")
	}
	for _, o := range outcomes {
		pr := o.PRURL
		if pr == "" {
			pr = "-"
		}
		if all {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
				codename.For(o.Repo, o.Issue), o.Repo, o.Issue, o.Outcome, pr, o.FinishedAt)
		} else {
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n",
				codename.For(o.Repo, o.Issue), o.Issue, o.Outcome, pr, o.FinishedAt)
		}
	}
	return w.Flush()
}
