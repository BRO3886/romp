package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/job"
)

type statusJSONJob struct {
	Repo           string `json:"repo"`
	Codename       string `json:"codename"`
	Issue          int    `json:"issue"`
	Branch         string `json:"branch"`
	ClaimedAt      string `json:"claimed_at"`
	ElapsedSeconds int64  `json:"elapsed_seconds"`
	SessionID      string `json:"session_id"`
}

func TestElapsed(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

	if got := elapsed("2026-08-18T11:55:00Z", now); got != "5m0s" {
		t.Errorf("elapsed = %q, want 5m0s", got)
	}
	if got := elapsed("2026-08-18T11:59:30Z", now); got != "30s" {
		t.Errorf("elapsed = %q, want 30s", got)
	}
	if got := elapsed("not-a-time", now); got != "?" {
		t.Errorf("elapsed unparseable = %q, want ?", got)
	}
}

func TestPrintJobsExposesSessionID(t *testing.T) {
	var out bytes.Buffer
	printJobs(&out, []job.Job{
		{Repo: "o/r", Issue: 7, Branch: "romp-7", ClaimedAt: time.Now().UTC().Format(time.RFC3339Nano), SessionID: "session-7"},
		{Repo: "o/r", Issue: 8, Branch: "romp-8", ClaimedAt: time.Now().UTC().Format(time.RFC3339Nano)},
	}, false)
	got := out.String()
	if tableColumn(t, got, "SESSION", 0) != "session-7" {
		t.Errorf("session row missing:\n%s", got)
	}
	if tableColumn(t, got, "SESSION", 1) != "-" {
		t.Errorf("empty session should be a dash:\n%s", got)
	}
	if !strings.Contains(got, "SESSION") {
		t.Errorf("header missing SESSION:\n%s", got)
	}
}

func TestWriteJobsJSONCalculatesElapsedSeconds(t *testing.T) {
	var out bytes.Buffer
	err := writeJobsJSON(&out, []job.Job{{
		Repo: "o/r", Issue: 7, Branch: "romp-7", ClaimedAt: "2026-08-18T11:58:30Z",
	}}, time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("writeJobsJSON: %v", err)
	}
	var got []statusJSONJob
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode status JSON: %v", err)
	}
	if len(got) != 1 || got[0].ElapsedSeconds != 90 {
		t.Errorf("status JSON = %+v, want elapsed_seconds 90", got)
	}
}

func TestStatusJSONScopesToCurrentRepo(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	owner, name, err := currentRepo(context.Background())
	if err != nil {
		t.Fatalf("currentRepo: %v", err)
	}
	repo := owner + "/" + name
	seedStatusJobs(t, []job.Job{
		{Repo: repo, Issue: 53, Branch: "romp-53", SessionID: "session-53"},
		{Repo: "other/repo", Issue: 7, Branch: "romp-7"},
	})

	got := executeStatusJSON(t, "--json")
	if len(got) != 1 {
		t.Fatalf("status --json returned %d jobs, want 1: %+v", len(got), got)
	}
	if got[0].Repo != repo || got[0].Issue != 53 || got[0].Branch != "romp-53" {
		t.Errorf("status --json job = %+v, want current repo issue 53", got[0])
	}
	if got[0].Codename == "" || got[0].ClaimedAt == "" || got[0].ElapsedSeconds < 0 {
		t.Errorf("status --json derived fields = %+v", got[0])
	}
	if got[0].SessionID != "session-53" {
		t.Errorf("session_id = %q, want session-53", got[0].SessionID)
	}
}

func TestStatusJSONAllIncludesEveryRepoAndEmptySession(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedStatusJobs(t, []job.Job{
		{Repo: "a/repo", Issue: 1, Branch: "romp-1", SessionID: "session-1"},
		{Repo: "z/repo", Issue: 2, Branch: "romp-2"},
	})

	got := executeStatusJSON(t, "--json", "--all")
	if len(got) != 2 {
		t.Fatalf("status --json --all returned %d jobs, want 2: %+v", len(got), got)
	}
	if got[0].Repo != "a/repo" || got[1].Repo != "z/repo" {
		t.Errorf("status --json --all repos = [%q %q], want [a/repo z/repo]", got[0].Repo, got[1].Repo)
	}
	if got[1].SessionID != "" {
		t.Errorf("missing session_id = %q, want empty string", got[1].SessionID)
	}
}

func TestStatusJSONEmptyResultIsArray(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cmd := newStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--json", "--all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status --json --all: %v", err)
	}
	if got := out.String(); got != "[]\n" {
		t.Errorf("empty status JSON = %q, want %q", got, "[]\n")
	}
}

func seedStatusJobs(t *testing.T, jobs []job.Job) {
	t.Helper()
	store, err := job.Open(job.Path())
	if err != nil {
		t.Fatalf("job.Open: %v", err)
	}
	defer store.Close()
	for _, j := range jobs {
		ok, err := store.Claim(context.Background(), j.Repo, j.Issue, j.Branch)
		if err != nil || !ok {
			t.Fatalf("Claim %s#%d: ok=%v err=%v", j.Repo, j.Issue, ok, err)
		}
		if j.SessionID != "" {
			if err := store.SetSessionID(context.Background(), j.Repo, j.Issue, j.SessionID); err != nil {
				t.Fatalf("SetSessionID %s#%d: %v", j.Repo, j.Issue, err)
			}
		}
	}
}

func executeStatusJSON(t *testing.T, args ...string) []statusJSONJob {
	t.Helper()
	cmd := newStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status %v: %v", args, err)
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw status JSON %q: %v", out.String(), err)
	}
	wantFields := []string{"repo", "codename", "issue", "branch", "claimed_at", "elapsed_seconds", "session_id"}
	for i, row := range raw {
		if len(row) != len(wantFields) {
			t.Errorf("status JSON row %d has %d fields, want %d: %v", i, len(row), len(wantFields), row)
		}
		for _, field := range wantFields {
			if _, ok := row[field]; !ok {
				t.Errorf("status JSON row %d missing %q: %v", i, field, row)
			}
		}
	}
	var got []statusJSONJob
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode status JSON %q: %v", out.String(), err)
	}
	return got
}
