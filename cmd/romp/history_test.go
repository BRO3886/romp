package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/job"
)

func TestHistoryCmdDefaultsToCurrentRepo(t *testing.T) {
	ctx := context.Background()
	owner, name, err := currentRepo(ctx)
	if err != nil {
		t.Fatalf("currentRepo: %v", err)
	}
	repo := owner + "/" + name
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedHistory(t, ctx, repo, 7)
	seedHistory(t, ctx, "other/repo", 8)

	cmd := newHistoryCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("history: %v", err)
	}

	got := out.String()
	if !hasField(got, "7") {
		t.Errorf("history output = %q, want current repo issue", got)
	}
	if strings.Contains(got, "other/repo") || hasField(got, "8") {
		t.Errorf("history output = %q, must exclude other repos", got)
	}
	if strings.Contains(got, "REPO") {
		t.Errorf("history output = %q, must omit REPO for one repo", got)
	}
}

func TestHistoryCmdAllListsEveryRepo(t *testing.T) {
	ctx := context.Background()
	owner, name, err := currentRepo(ctx)
	if err != nil {
		t.Fatalf("currentRepo: %v", err)
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedHistory(t, ctx, owner+"/"+name, 7)
	seedHistory(t, ctx, "other/repo", 8)

	cmd := newHistoryCmd()
	cmd.SetArgs([]string{"--all"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("history --all: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "REPO") || !strings.Contains(got, "other/repo") || !hasField(got, "7") || !hasField(got, "8") {
		t.Errorf("history --all output = %q, want both repos with REPO column", got)
	}
}

func hasField(output, field string) bool {
	for _, value := range strings.Fields(output) {
		if value == field {
			return true
		}
	}
	return false
}

func seedHistory(t *testing.T, ctx context.Context, repo string, issue int) {
	t.Helper()
	store, err := job.Open(job.Path())
	if err != nil {
		t.Fatalf("open job store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if ok, err := store.Claim(ctx, repo, issue, "romp"); err != nil || !ok {
		t.Fatalf("Claim %s #%d: ok=%v err=%v", repo, issue, ok, err)
	}
	if err := store.Finish(ctx, job.Outcome{Repo: repo, Issue: issue, Outcome: "done", FinishedAt: "2026-08-18T12:00:00Z"}); err != nil {
		t.Fatalf("Finish %s #%d: %v", repo, issue, err)
	}
}
