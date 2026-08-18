package job

import (
	"context"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestClaimAndDelete(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	ok, err := s.Claim(ctx, "o/r", 7, "romp-7")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !ok {
		t.Fatal("first Claim = false, want true")
	}

	ok, err = s.Claim(ctx, "o/r", 7, "romp-7")
	if err != nil {
		t.Fatalf("second Claim: %v", err)
	}
	if ok {
		t.Fatal("second Claim = true, want false (duplicate)")
	}

	if err := s.Delete(ctx, "o/r", 7); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	ok, err = s.Claim(ctx, "o/r", 7, "romp-7")
	if err != nil {
		t.Fatalf("re-Claim: %v", err)
	}
	if !ok {
		t.Fatal("Claim after Delete = false, want true")
	}
}

func TestClaimIsScopedByRepo(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	if ok, err := s.Claim(ctx, "o/r", 7, "romp-7"); err != nil || !ok {
		t.Fatalf("Claim o/r: ok=%v err=%v", ok, err)
	}
	if ok, err := s.Claim(ctx, "other/r", 7, "romp-7"); err != nil || !ok {
		t.Fatalf("Claim other/r: ok=%v err=%v, want true (different repo)", ok, err)
	}
}

func TestClearRunning(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	for _, n := range []int{1, 2, 3} {
		if ok, err := s.Claim(ctx, "o/r", n, "romp-1"); err != nil || !ok {
			t.Fatalf("Claim %d: ok=%v err=%v", n, ok, err)
		}
	}
	if err := s.ClearRunning(ctx); err != nil {
		t.Fatalf("ClearRunning: %v", err)
	}
	if ok, err := s.Claim(ctx, "o/r", 1, "romp-1"); err != nil || !ok {
		t.Fatalf("Claim after clear: ok=%v err=%v, want true", ok, err)
	}
}

func TestList(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	for _, n := range []int{3, 1, 2} {
		if ok, err := s.Claim(ctx, "o/r", n, "romp-3"); err != nil || !ok {
			t.Fatalf("Claim %d: ok=%v err=%v", n, ok, err)
		}
	}

	jobs, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("List len = %d, want 3", len(jobs))
	}
	for i, want := range []int{1, 2, 3} {
		if jobs[i].Issue != want {
			t.Errorf("jobs[%d].Issue = %d, want %d (ordered by issue)", i, jobs[i].Issue, want)
		}
		if jobs[i].Repo != "o/r" || jobs[i].Branch != "romp-3" {
			t.Errorf("jobs[%d] = %+v, want repo o/r branch romp-3", i, jobs[i])
		}
		if jobs[i].ClaimedAt == "" {
			t.Errorf("jobs[%d].ClaimedAt empty", i)
		}
	}
}
