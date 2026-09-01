package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/review"
)

func TestWriteHistorySingleRepoOmitsRepoColumn(t *testing.T) {
	var out bytes.Buffer
	if err := writeHistory(&out, []job.Outcome{
		{Repo: "a/b", Issue: 7, Outcome: "done", FinishedAt: "2026-08-18T12:00:00Z"},
	}, false); err != nil {
		t.Fatalf("writeHistory: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "REPO") || strings.Contains(got, "a/b") {
		t.Errorf("single-repo table should omit REPO:\n%s", got)
	}
	if col := tableColumn(t, got, "ISSUE", 0); col != "7" {
		t.Errorf("ISSUE = %q, want 7\n%s", col, got)
	}
	if col := tableColumn(t, got, "SESSION", 0); col != "-" {
		t.Errorf("SESSION = %q, want -\n%s", col, got)
	}
}

func TestWriteHistoryExposesSessionID(t *testing.T) {
	var out bytes.Buffer
	if err := writeHistory(&out, []job.Outcome{
		{Repo: "a/b", Issue: 7, Outcome: "red", FinishedAt: "2026-08-18T12:00:00Z", SessionID: "session-7"},
	}, false); err != nil {
		t.Fatalf("writeHistory: %v", err)
	}
	if got := tableColumn(t, out.String(), "SESSION", 0); got != "session-7" {
		t.Errorf("SESSION = %q, want session-7\n%s", got, out.String())
	}
}

func TestWriteHistoryAllIncludesEveryRepo(t *testing.T) {
	var out bytes.Buffer
	if err := writeHistory(&out, []job.Outcome{
		{Repo: "a/b", Issue: 7, Outcome: "done", FinishedAt: "t1"},
		{Repo: "other/repo", Issue: 8, Outcome: "red", FinishedAt: "t2"},
	}, true); err != nil {
		t.Fatalf("writeHistory: %v", err)
	}
	got := out.String()
	if tableColumn(t, got, "REPO", 0) != "a/b" || tableColumn(t, got, "ISSUE", 0) != "7" {
		t.Errorf("row 0 = %q", got)
	}
	if tableColumn(t, got, "REPO", 1) != "other/repo" || tableColumn(t, got, "ISSUE", 1) != "8" {
		t.Errorf("row 1 = %q", got)
	}
}

func TestHistoryCmdHasAllFlag(t *testing.T) {
	for _, name := range []string{"all", "review", "days"} {
		if newHistoryCmd().Flags().Lookup(name) == nil {
			t.Fatalf("history is missing --%s", name)
		}
	}
}

func TestWriteReviewSummary(t *testing.T) {
	var out bytes.Buffer
	writeReviewSummary(&out, job.ReviewSummary{
		ReviewedJobs: 4, CleanPassJobs: 3, FixRoundJobs: 1, MedianReviewerDuration: 1250 * time.Millisecond,
	})
	got := out.String()
	for _, want := range []string{"reviewed jobs: 4", "clean-pass rate: 75.0%", "fix-round rate: 25.0%", "median reviewer duration: 1.25s"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestHistoryReviewCommandReportsStoredJobs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	owner, name, err := currentRepo(context.Background())
	if err != nil {
		t.Fatalf("currentRepo: %v", err)
	}
	store, err := job.Open(job.Path())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	repo := owner + "/" + name
	if ok, err := store.Claim(context.Background(), repo, 34, "romp-34"); err != nil || !ok {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	metrics := review.Instrumentation{ReviewRan: true, Passes: []review.PassInstrumentation{{Verdict: review.VerdictApprove, DurationMS: 1250}}}
	if err := store.SetReviewInstrumentation(context.Background(), repo, 34, metrics); err != nil {
		t.Fatalf("SetReviewInstrumentation: %v", err)
	}
	if err := store.Finish(context.Background(), job.Outcome{Repo: repo, Issue: 34, Outcome: "done", FinishedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	cmd := newHistoryCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--review", "--days", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("history --review: %v", err)
	}
	for _, want := range []string{"reviewed jobs: 1", "clean-pass rate: 100.0%", "fix-round rate: 0.0%", "median reviewer duration: 1.25s"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func tableColumn(t *testing.T, output, column string, row int) string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) < row+2 {
		t.Fatalf("need header + row %d, got %d lines:\n%s", row, len(lines), output)
	}
	headers := strings.Fields(lines[0])
	idx := -1
	for i, h := range headers {
		if h == column {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no column %q in %v", column, headers)
	}
	fields := strings.Fields(lines[row+1])
	if idx >= len(fields) {
		t.Fatalf("row %d has no field %d: %v", row, idx, fields)
	}
	return fields[idx]
}
