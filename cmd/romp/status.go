package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/codename"
	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/job"
)

func newStatusCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "List in-flight jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd.Context(), all)
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "list jobs across every repo on this machine")
	return cmd
}

func runStatus(ctx context.Context, all bool) error {
	if all {
		jobs, err := allJobs(ctx)
		if err != nil {
			return err
		}
		printJobs(jobs, true)
		return nil
	}
	_, _, jobs, err := loadJobs(ctx)
	if err != nil {
		return err
	}
	printJobs(jobs, false)
	return nil
}

// allJobs reads every in-flight job table on this machine, one per repo.
func allJobs(ctx context.Context) ([]job.Job, error) {
	dbs, err := job.DBs()
	if err != nil {
		return nil, err
	}
	var out []job.Job
	for _, db := range dbs {
		store, err := job.Open(db)
		if err != nil {
			return nil, err
		}
		jobs, err := store.List(ctx)
		store.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, jobs...)
	}
	return out, nil
}

func printJobs(jobs []job.Job, withRepo bool) {
	if len(jobs) == 0 {
		fmt.Println("no in-flight jobs")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if withRepo {
		fmt.Fprintln(w, "REPO\tCODENAME\tISSUE\tBRANCH\tELAPSED")
		for _, j := range jobs {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", j.Repo, codename.For(j.Repo, j.Issue), j.Issue, j.Branch, elapsed(j.ClaimedAt, time.Now()))
		}
	} else {
		fmt.Fprintln(w, "CODENAME\tISSUE\tBRANCH\tELAPSED")
		for _, j := range jobs {
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", codename.For(j.Repo, j.Issue), j.Issue, j.Branch, elapsed(j.ClaimedAt, time.Now()))
		}
	}
	w.Flush()
}

// elapsed renders the time since a claim timestamp, or "?" when unparseable.
func elapsed(claimedAt string, now time.Time) string {
	t, err := time.Parse(time.RFC3339Nano, claimedAt)
	if err != nil {
		return "?"
	}
	return now.Sub(t).Round(time.Second).String()
}

// loadJobs resolves the current repo and reads its in-flight job table.
func loadJobs(ctx context.Context) (owner, name string, jobs []job.Job, err error) {
	root, err := repoRoot(ctx)
	if err != nil {
		return "", "", nil, err
	}
	owner, name, err = (&git.Repo{Root: root}).Origin(ctx)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve origin: %w", err)
	}
	store, err := job.Open(job.Path(owner, name))
	if err != nil {
		return "", "", nil, err
	}
	defer store.Close()
	jobs, err = store.List(ctx)
	if err != nil {
		return "", "", nil, err
	}
	return owner, name, jobs, nil
}
