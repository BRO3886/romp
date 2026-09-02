package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/codename"
	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/job"
)

func newStatusCmd() *cobra.Command {
	var (
		all        bool
		jsonOutput bool
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "List in-flight jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd.Context(), cmd.OutOrStdout(), all, jsonOutput)
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "list jobs across every repo on this machine")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print jobs as JSON")
	return cmd
}

func runStatus(ctx context.Context, out io.Writer, all, jsonOutput bool) error {
	store, err := job.Open(job.Path())
	if err != nil {
		return err
	}
	defer store.Close()

	repo := ""
	if !all {
		owner, name, err := currentRepo(ctx)
		if err != nil {
			return err
		}
		repo = owner + "/" + name
	}
	jobs, err := store.List(ctx, repo)
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJobsJSON(out, jobs, time.Now())
	}
	printJobs(out, jobs, all)
	return nil
}

type statusJob struct {
	Repo           string `json:"repo"`
	Codename       string `json:"codename"`
	Issue          int    `json:"issue"`
	Branch         string `json:"branch"`
	ClaimedAt      string `json:"claimed_at"`
	ElapsedSeconds int64  `json:"elapsed_seconds"`
	SessionID      string `json:"session_id"`
}

func writeJobsJSON(out io.Writer, jobs []job.Job, now time.Time) error {
	rows := make([]statusJob, 0, len(jobs))
	for _, j := range jobs {
		claimedAt, err := time.Parse(time.RFC3339Nano, j.ClaimedAt)
		if err != nil {
			return fmt.Errorf("parsing claimed_at for %s#%d: %w", j.Repo, j.Issue, err)
		}
		rows = append(rows, statusJob{
			Repo:           j.Repo,
			Codename:       codename.For(j.Repo, j.Issue),
			Issue:          j.Issue,
			Branch:         j.Branch,
			ClaimedAt:      j.ClaimedAt,
			ElapsedSeconds: int64(now.Sub(claimedAt).Round(time.Second) / time.Second),
			SessionID:      j.SessionID,
		})
	}
	if err := json.NewEncoder(out).Encode(rows); err != nil {
		return fmt.Errorf("encoding status JSON: %w", err)
	}
	return nil
}

func printJobs(out io.Writer, jobs []job.Job, withRepo bool) {
	if len(jobs) == 0 {
		fmt.Fprintln(out, "no in-flight jobs")
		return
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if withRepo {
		fmt.Fprintln(w, "REPO\tCODENAME\tISSUE\tBRANCH\tELAPSED\tSESSION")
		for _, j := range jobs {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n", j.Repo, codename.For(j.Repo, j.Issue), j.Issue, j.Branch, elapsed(j.ClaimedAt, time.Now()), valueOrDash(j.SessionID))
		}
	} else {
		fmt.Fprintln(w, "CODENAME\tISSUE\tBRANCH\tELAPSED\tSESSION")
		for _, j := range jobs {
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n", codename.For(j.Repo, j.Issue), j.Issue, j.Branch, elapsed(j.ClaimedAt, time.Now()), valueOrDash(j.SessionID))
		}
	}
	w.Flush()
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// elapsed renders the time since a claim timestamp, or "?" when unparseable.
func elapsed(claimedAt string, now time.Time) string {
	t, err := time.Parse(time.RFC3339Nano, claimedAt)
	if err != nil {
		return "?"
	}
	return now.Sub(t).Round(time.Second).String()
}

// currentRepo resolves the origin of the working directory.
func currentRepo(ctx context.Context) (owner, name string, err error) {
	root, err := repoRoot(ctx)
	if err != nil {
		return "", "", err
	}
	owner, name, err = (&git.Repo{Root: root}).Origin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("resolve origin: %w", err)
	}
	return owner, name, nil
}
