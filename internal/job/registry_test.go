package job

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRegistrySurvivesReopenAndRejectsDuplicateOrigin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "romp.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Watch(ctx, Repository{Origin: "o/r", Path: "/repo/one"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Watch(ctx, Repository{Origin: "o/r", Path: "/repo/two"}); err == nil {
		t.Fatal("duplicate origin accepted")
	}
	if err := s.Watch(ctx, Repository{Origin: "o/r", Path: "/repo/one"}); err != nil {
		t.Fatal("idempotent watch:", err)
	}
	if err := s.Rebind(ctx, Repository{Origin: "o/r", Path: "/repo/two"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repos, err := s.Repositories(ctx)
	if err != nil || len(repos) != 1 || repos[0].Path != "/repo/two" {
		t.Fatalf("registry=%+v err=%v", repos, err)
	}
	if err := s.Unwatch(ctx, "o/r"); err != nil {
		t.Fatal(err)
	}
	repos, err = s.Repositories(ctx)
	if err != nil || len(repos) != 0 {
		t.Fatalf("registry=%+v err=%v", repos, err)
	}
}

func TestRecoveryPreservesSessionAndRecordsInterruptedOnce(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "romp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, repo := range []string{"one/repo", "two/repo"} {
		if ok, err := s.Claim(ctx, repo, 7, "romp-7"); !ok || err != nil {
			t.Fatal(ok, err)
		}
		if err := s.SetSessionID(ctx, repo, 7, "session-7"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.InterruptRunning(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.InterruptRunning(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := s.List(ctx, "")
	if err != nil || len(active) != 0 {
		t.Fatal(active, err)
	}
	history, err := s.History(ctx, "", 20)
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	for _, o := range history {
		if o.Outcome != "interrupted" || o.SessionID != "session-7" || o.Branch != "romp-7" || o.StartedAt == "" {
			t.Fatalf("recovery outcome=%+v", o)
		}
	}
}
