package main

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/codename"
	"github.com/BRO3886/romp/internal/job"
)

func newHistoryCmd() *cobra.Command {
	var (
		all        bool
		reviewOnly bool
		days       int
	)
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show recently finished jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHistory(cmd, all, reviewOnly, days)
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "list jobs across every repo on this machine")
	cmd.Flags().BoolVar(&reviewOnly, "review", false, "show review-gate calibration instead of job rows")
	cmd.Flags().IntVar(&days, "days", 30, "history window in days for --review")
	return cmd
}

func runHistory(cmd *cobra.Command, all, reviewOnly bool, days int) error {
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
	if reviewOnly {
		if days < 1 {
			return fmt.Errorf("days must be at least 1")
		}
		summary, err := store.ReviewSummary(cmd.Context(), repo, time.Now().AddDate(0, 0, -days))
		if err != nil {
			return err
		}
		writeReviewSummary(cmd.OutOrStdout(), summary)
		return nil
	}
	outcomes, err := store.History(cmd.Context(), repo, 20)
	if err != nil {
		return err
	}
	if len(outcomes) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no jobs finished yet")
		return nil
	}
	return writeHistory(cmd.OutOrStdout(), outcomes, all)
}

func writeReviewSummary(out io.Writer, summary job.ReviewSummary) {
	cleanRate, fixRate := 0.0, 0.0
	if summary.ReviewedJobs > 0 {
		cleanRate = 100 * float64(summary.CleanPassJobs) / float64(summary.ReviewedJobs)
		fixRate = 100 * float64(summary.FixRoundJobs) / float64(summary.ReviewedJobs)
	}
	fmt.Fprintf(out, "reviewed jobs: %d\n", summary.ReviewedJobs)
	fmt.Fprintf(out, "clean-pass rate: %.1f%%\n", cleanRate)
	fmt.Fprintf(out, "fix-round rate: %.1f%%\n", fixRate)
	fmt.Fprintf(out, "median reviewer duration: %s\n", summary.MedianReviewerDuration)
}

func writeHistory(out io.Writer, outcomes []job.Outcome, all bool) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if all {
		fmt.Fprintln(w, "CODENAME\tREPO\tISSUE\tOUTCOME\tPR\tFINISHED\tSESSION")
	} else {
		fmt.Fprintln(w, "CODENAME\tISSUE\tOUTCOME\tPR\tFINISHED\tSESSION")
	}
	for _, o := range outcomes {
		pr := o.PRURL
		if pr == "" {
			pr = "-"
		}
		if all {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
				codename.For(o.Repo, o.Issue), o.Repo, o.Issue, o.Outcome, pr, o.FinishedAt, valueOrDash(o.SessionID))
		} else {
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n",
				codename.For(o.Repo, o.Issue), o.Issue, o.Outcome, pr, o.FinishedAt, valueOrDash(o.SessionID))
		}
	}
	return w.Flush()
}
