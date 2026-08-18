package main

import (
	"context"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/job"
)

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

func TestAllJobsAggregatesAcrossRepos(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	ctx := context.Background()

	for _, repo := range []struct{ owner, name string }{{"a", "b"}, {"c", "d"}} {
		s, err := job.Open(job.Path(repo.owner, repo.name))
		if err != nil {
			t.Fatalf("Open %s: %v", repo.owner+"/"+repo.name, err)
		}
		if _, err := s.Claim(ctx, repo.owner+"/"+repo.name, 7, "romp-7"); err != nil {
			t.Fatalf("Claim %s: %v", repo.owner+"/"+repo.name, err)
		}
		s.Close()
	}

	jobs, err := allJobs(ctx)
	if err != nil {
		t.Fatalf("allJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("allJobs len = %d, want 2 (%v)", len(jobs), jobs)
	}
	repos := map[string]bool{}
	for _, j := range jobs {
		repos[j.Repo] = true
	}
	if !repos["a/b"] || !repos["c/d"] {
		t.Errorf("allJobs repos = %v, want a/b and c/d", repos)
	}
}
