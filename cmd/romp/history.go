package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/codename"
	"github.com/BRO3886/romp/internal/job"
)

func newHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history",
		Short: "Show recently finished jobs",
		Args:  cobra.NoArgs,
		RunE:  runHistory,
	}
}

func runHistory(cmd *cobra.Command, _ []string) error {
	store, err := job.Open(job.Path())
	if err != nil {
		return err
	}
	defer store.Close()

	outcomes, err := store.History(cmd.Context(), 20)
	if err != nil {
		return err
	}
	if len(outcomes) == 0 {
		fmt.Println("no jobs finished yet")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "CODENAME\tREPO\tISSUE\tOUTCOME\tPR\tFINISHED")
	for _, o := range outcomes {
		pr := o.PRURL
		if pr == "" {
			pr = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
			codename.For(o.Repo, o.Issue), o.Repo, o.Issue, o.Outcome, pr, o.FinishedAt)
	}
	return w.Flush()
}
