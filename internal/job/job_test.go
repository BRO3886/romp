package job

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "romp.db"))
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

func TestClearRunningIsScopedByRepo(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	for _, n := range []int{1, 2, 3} {
		if ok, err := s.Claim(ctx, "o/r", n, "romp-1"); err != nil || !ok {
			t.Fatalf("Claim %d: ok=%v err=%v", n, ok, err)
		}
	}
	if ok, err := s.Claim(ctx, "other/r", 1, "romp-1"); err != nil || !ok {
		t.Fatalf("Claim other/r: ok=%v err=%v", ok, err)
	}
	if err := s.ClearRunning(ctx, "o/r"); err != nil {
		t.Fatalf("ClearRunning: %v", err)
	}
	if ok, err := s.Claim(ctx, "o/r", 1, "romp-1"); err != nil || !ok {
		t.Fatalf("Claim after clear: ok=%v err=%v, want true", ok, err)
	}
	if ok, err := s.Claim(ctx, "other/r", 1, "romp-1"); err != nil || ok {
		t.Fatalf("Claim other/r after clear: ok=%v err=%v, want false (row must survive)", ok, err)
	}
}

func TestListFiltersByRepo(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	if ok, err := s.Claim(ctx, "o/r", 7, "romp-7"); err != nil || !ok {
		t.Fatalf("Claim o/r: ok=%v err=%v", ok, err)
	}
	if ok, err := s.Claim(ctx, "other/r", 3, "romp-3"); err != nil || !ok {
		t.Fatalf("Claim other/r: ok=%v err=%v", ok, err)
	}

	jobs, err := s.List(ctx, "o/r")
	if err != nil {
		t.Fatalf("List o/r: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Repo != "o/r" || jobs[0].Issue != 7 {
		t.Errorf("List(o/r) = %v, want only o/r #7", jobs)
	}

	all, err := s.List(ctx, "")
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List(all) len = %d, want 2", len(all))
	}
}

func TestFinishMovesRowToHistory(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	if ok, err := s.Claim(ctx, "o/r", 7, "romp-7"); err != nil || !ok {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	if err := s.SetSessionID(ctx, "o/r", 7, "session-7"); err != nil {
		t.Fatalf("SetSessionID: %v", err)
	}

	if err := s.Finish(ctx, Outcome{
		Repo: "o/r", Issue: 7, Outcome: "done", Branch: "romp-7",
		PRURL: "https://github.com/o/r/pull/1", FinishedAt: "2026-08-18T12:00:00Z",
	}); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	jobs, err := s.List(ctx, "o/r")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("in-flight rows after finish = %v, want none", jobs)
	}

	outcomes, err := s.History(ctx, "", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("History len = %d, want 1", len(outcomes))
	}
	o := outcomes[0]
	if o.Outcome != "done" || o.PRURL != "https://github.com/o/r/pull/1" || o.Branch != "romp-7" {
		t.Errorf("outcome = %+v, want done with PR URL and branch", o)
	}
	if o.StartedAt == "" {
		t.Error("StartedAt empty, want the carried-over claim timestamp")
	}
	if o.FinishedAt != "2026-08-18T12:00:00Z" {
		t.Errorf("FinishedAt = %q, want the recorded time", o.FinishedAt)
	}
	if o.SessionID != "session-7" {
		t.Errorf("SessionID = %q, want session-7", o.SessionID)
	}
}

func TestSetSessionIDExposesActiveJob(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if ok, err := s.Claim(ctx, "o/r", 7, "romp-7"); err != nil || !ok {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	if err := s.SetSessionID(ctx, "o/r", 7, "session-7"); err != nil {
		t.Fatalf("SetSessionID: %v", err)
	}
	jobs, err := s.List(ctx, "o/r")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 || jobs[0].SessionID != "session-7" {
		t.Fatalf("jobs = %+v, want active session-7", jobs)
	}
}

func TestOpenMigratesLegacyDatabaseWithoutDataLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "romp.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	legacySchema := `
CREATE TABLE jobs (
	id TEXT PRIMARY KEY, repo TEXT NOT NULL, issue INTEGER NOT NULL,
	branch TEXT NOT NULL, claimed_at TEXT NOT NULL, UNIQUE(repo, issue)
);
CREATE TABLE outcomes (
	id INTEGER PRIMARY KEY AUTOINCREMENT, repo TEXT NOT NULL, issue INTEGER NOT NULL,
	outcome TEXT NOT NULL, branch TEXT NOT NULL, pr_url TEXT, detail TEXT,
	started_at TEXT NOT NULL, finished_at TEXT NOT NULL
);
INSERT INTO jobs (id, repo, issue, branch, claimed_at)
	VALUES ('old-job', 'o/r', 7, 'romp-7', '2026-08-18T11:00:00Z');
INSERT INTO outcomes (repo, issue, outcome, branch, started_at, finished_at)
	VALUES ('o/r', 6, 'done', 'romp-6', '2026-08-18T09:00:00Z', '2026-08-18T10:00:00Z');`
	if _, err := db.Exec(legacySchema); err != nil {
		db.Close()
		t.Fatalf("creating legacy database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing legacy database: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open migrated database: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	jobs, err := s.List(ctx, "o/r")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Issue != 7 || jobs[0].SessionID != "" {
		t.Fatalf("legacy jobs = %+v", jobs)
	}
	outcomes, err := s.History(ctx, "o/r", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Issue != 6 || outcomes[0].SessionID != "" {
		t.Fatalf("legacy outcomes = %+v", outcomes)
	}
	if err := s.SetSessionID(ctx, "o/r", 7, "migrated-session"); err != nil {
		t.Fatalf("SetSessionID after migration: %v", err)
	}
	if err := s.Finish(ctx, Outcome{
		Repo: "o/r", Issue: 7, Outcome: "red", Branch: "romp-7", FinishedAt: "2026-08-18T12:00:00Z",
	}); err != nil {
		t.Fatalf("Finish after migration: %v", err)
	}
	outcomes, err = s.History(ctx, "o/r", 10)
	if err != nil {
		t.Fatalf("History after Finish: %v", err)
	}
	if len(outcomes) != 2 || outcomes[0].SessionID != "migrated-session" || outcomes[1].Issue != 6 {
		t.Fatalf("migrated outcomes = %+v", outcomes)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second idempotent Open: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("closing second store: %v", err)
	}
}

func TestFinishWithoutInFlightRowIsNoOp(t *testing.T) {
	s := openTest(t)
	if err := s.Finish(context.Background(), Outcome{Repo: "o/r", Issue: 7, Outcome: "done"}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	outcomes, err := s.History(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(outcomes) != 0 {
		t.Errorf("History len = %d, want 0", len(outcomes))
	}
}

func TestHistoryNewestFirst(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	for _, n := range []int{7, 8} {
		if ok, err := s.Claim(ctx, "o/r", n, "romp-7"); err != nil || !ok {
			t.Fatalf("Claim %d: ok=%v err=%v", n, ok, err)
		}
	}
	if err := s.Finish(ctx, Outcome{Repo: "o/r", Issue: 7, Outcome: "done", FinishedAt: "2026-08-18T12:00:00Z"}); err != nil {
		t.Fatalf("Finish 7: %v", err)
	}
	if err := s.Finish(ctx, Outcome{Repo: "o/r", Issue: 8, Outcome: "no-changes", FinishedAt: "2026-08-18T13:00:00Z"}); err != nil {
		t.Fatalf("Finish 8: %v", err)
	}

	outcomes, err := s.History(ctx, "", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("History len = %d, want 2", len(outcomes))
	}
	if outcomes[0].Issue != 8 || outcomes[1].Issue != 7 {
		t.Errorf("history order = [%d %d], want newest first [8 7]", outcomes[0].Issue, outcomes[1].Issue)
	}
}

func TestPruneDeletesOnlyOldOutcomes(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	for _, n := range []int{7, 8} {
		if ok, err := s.Claim(ctx, "o/r", n, "romp-7"); err != nil || !ok {
			t.Fatalf("Claim %d: ok=%v err=%v", n, ok, err)
		}
	}
	if err := s.Finish(ctx, Outcome{Repo: "o/r", Issue: 7, Outcome: "done", FinishedAt: "2026-08-01T00:00:00Z"}); err != nil {
		t.Fatalf("Finish 7: %v", err)
	}
	if err := s.Finish(ctx, Outcome{Repo: "o/r", Issue: 8, Outcome: "done", FinishedAt: "2026-08-18T00:00:00Z"}); err != nil {
		t.Fatalf("Finish 8: %v", err)
	}

	cutoff := "2026-08-15T00:00:00Z"
	n, err := s.CountBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("CountBefore: %v", err)
	}
	if n != 1 {
		t.Errorf("CountBefore = %d, want 1", n)
	}

	pruned, err := s.Prune(ctx, cutoff)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if pruned != 1 {
		t.Errorf("Prune = %d, want 1", pruned)
	}

	outcomes, err := s.History(ctx, "", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Issue != 8 {
		t.Errorf("History after prune = %v, want only issue 8", outcomes)
	}
}

func TestHistoryFiltersByRepo(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	for _, outcome := range []Outcome{
		{Repo: "o/r", Issue: 7, Outcome: "done", FinishedAt: "2026-08-18T12:00:00Z"},
		{Repo: "other/r", Issue: 8, Outcome: "done", FinishedAt: "2026-08-18T13:00:00Z"},
	} {
		if ok, err := s.Claim(ctx, outcome.Repo, outcome.Issue, "romp"); err != nil || !ok {
			t.Fatalf("Claim %s #%d: ok=%v err=%v", outcome.Repo, outcome.Issue, ok, err)
		}
		if err := s.Finish(ctx, outcome); err != nil {
			t.Fatalf("Finish %s #%d: %v", outcome.Repo, outcome.Issue, err)
		}
	}

	outcomes, err := s.History(ctx, "o/r", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Repo != "o/r" || outcomes[0].Issue != 7 {
		t.Errorf("History(o/r) = %v, want only o/r #7", outcomes)
	}

	all, err := s.History(ctx, "", 10)
	if err != nil {
		t.Fatalf("History all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("History(all) len = %d, want 2", len(all))
	}
}
