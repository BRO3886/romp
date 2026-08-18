package watch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/runner"
)

type fakeGH struct {
	mu      sync.Mutex
	issues  []gh.Issue
	added   []string
	removed []string
	addErr  error
}

func (f *fakeGH) ListIssues(context.Context, string, string) ([]gh.Issue, error) {
	return f.issues, nil
}

func (f *fakeGH) AddLabel(_ context.Context, _ string, number int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	f.added = append(f.added, fmt.Sprintf("%d:%s", number, label))
	return nil
}

func (f *fakeGH) RemoveLabel(_ context.Context, _ string, number int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, fmt.Sprintf("%d:%s", number, label))
	return nil
}

func (f *fakeGH) snapshot() (added, removed []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.added...), append([]string(nil), f.removed...)
}

type fakeStore struct {
	mu       sync.Mutex
	rows     map[int]bool
	finished []job.Outcome
}

func newFakeStore() *fakeStore { return &fakeStore{rows: map[int]bool{}} }

func (f *fakeStore) Claim(_ context.Context, _ string, issue int, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows[issue] {
		return false, nil
	}
	f.rows[issue] = true
	return true, nil
}

func (f *fakeStore) Delete(_ context.Context, _ string, issue int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, issue)
	return nil
}

func (f *fakeStore) ClearRunning(context.Context, string) error { return nil }

func (f *fakeStore) Finish(_ context.Context, o job.Outcome) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = append(f.finished, o)
	return nil
}

func (f *fakeStore) has(issue int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[issue]
}

func TestClaimBatchDispatchesUnclaimedOnly(t *testing.T) {
	ghc := &fakeGH{issues: []gh.Issue{
		{Number: 1},
		{Number: 2, Labels: []string{"romp:claimed"}},
		{Number: 3, Labels: []string{"romp:blocked"}},
		{Number: 4},
	}}
	store := newFakeStore()

	w := &Watcher{
		Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", Blocked: "romp:blocked",
		Width: 10, GH: ghc, Store: store,
	}

	var mu sync.Mutex
	var called []int
	var jobWG sync.WaitGroup
	jobWG.Add(2)
	w.RunJob = func(_ context.Context, issue int) (string, error) {
		mu.Lock()
		called = append(called, issue)
		mu.Unlock()
		jobWG.Done()
		return "", nil
	}

	var wg sync.WaitGroup
	if err := w.claimBatch(context.Background(), make(chan struct{}, 10), &wg, context.Background()); err != nil {
		t.Fatal(err)
	}
	jobWG.Wait()
	wg.Wait()

	sort.Ints(called)
	if len(called) != 2 || called[0] != 1 || called[1] != 4 {
		t.Errorf("RunJob called for %v, want [1 4]", called)
	}

	added, removed := ghc.snapshot()
	if !contains(added, "1:romp:claimed") || !contains(added, "4:romp:claimed") {
		t.Errorf("claim labels added = %v, want 1 and 4 claimed", added)
	}
	if contains(added, "2:romp:claimed") || contains(added, "3:romp:claimed") {
		t.Errorf("claim label added for already-claimed/blocked issue: %v", added)
	}
	if !contains(removed, "1:romp:claimed") || !contains(removed, "4:romp:claimed") {
		t.Errorf("claim labels released on done = %v, want 1 and 4", removed)
	}
}

func TestClaimBatchStopsAtWidth(t *testing.T) {
	ghc := &fakeGH{issues: []gh.Issue{{Number: 1}, {Number: 2}, {Number: 3}}}
	store := newFakeStore()

	w := &Watcher{
		Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", Blocked: "romp:blocked",
		Width: 1, GH: ghc, Store: store,
	}
	w.RunJob = func(context.Context, int) (string, error) { return "", nil }

	var wg sync.WaitGroup
	if err := w.claimBatch(context.Background(), make(chan struct{}, 1), &wg, context.Background()); err != nil {
		t.Fatal(err)
	}

	added, _ := ghc.snapshot()
	if len(added) != 1 {
		t.Errorf("claimed %d issues at width 1, want 1", len(added))
	}
}

func TestClaimRollsBackRowOnLabelFailure(t *testing.T) {
	ghc := &fakeGH{addErr: errors.New("boom")}
	store := newFakeStore()
	w := &Watcher{Repo: "o/r", Claim: "romp:claimed", GH: ghc, Store: store}

	if w.claim(context.Background(), gh.Issue{Number: 7}) {
		t.Fatal("claim = true, want false when AddLabel fails")
	}
	if store.has(7) {
		t.Fatal("row not rolled back after AddLabel failure")
	}
}

func TestRunJobReleasesOnDone(t *testing.T) {
	ghc := &fakeGH{}
	store := newFakeStore()
	store.rows[7] = true

	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	w.RunJob = func(context.Context, int) (string, error) { return "", nil }

	w.runJobSync(t, 7)

	if store.has(7) {
		t.Error("in-flight row not deleted")
	}
	_, removed := ghc.snapshot()
	if !contains(removed, "7:romp:claimed") {
		t.Errorf("claim label not released: %v", removed)
	}
	// The runner removes the trigger label as its completion marker, so watch
	// must not remove it a second time.
	if contains(removed, "7:romp") {
		t.Errorf("watch removed the trigger label the runner owns: %v", removed)
	}
}

func TestRunJobKeepsTriggerOnBlocked(t *testing.T) {
	ghc := &fakeGH{}
	store := newFakeStore()
	store.rows[7] = true

	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	w.RunJob = func(context.Context, int) (string, error) { return "", fmt.Errorf("%w: gap", runner.ErrBlocked) }

	w.runJobSync(t, 7)

	_, removed := ghc.snapshot()
	if contains(removed, "7:romp") {
		t.Errorf("trigger label removed on blocked: %v", removed)
	}
	if !contains(removed, "7:romp:claimed") {
		t.Errorf("claim label not released on blocked: %v", removed)
	}
}

func TestRunJobLogsTimeout(t *testing.T) {
	ghc := &fakeGH{}
	store := newFakeStore()
	store.rows[7] = true

	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	var logMsg string
	w.Logf = func(format string, a ...any) { logMsg = fmt.Sprintf(format, a...) }
	w.RunJob = func(context.Context, int) (string, error) { return "", fmt.Errorf("%w: killed", runner.ErrTimeout) }

	w.runJobSync(t, 7)

	if !strings.Contains(logMsg, "timeout") {
		t.Errorf("log = %q, want timeout", logMsg)
	}
	_, removed := ghc.snapshot()
	if !contains(removed, "7:romp:claimed") {
		t.Errorf("claim label not released on timeout: %v", removed)
	}
}

func TestRunJobRecordsHistory(t *testing.T) {
	ghc := &fakeGH{}
	store := newFakeStore()
	store.rows[7] = true

	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	w.RunJob = func(context.Context, int) (string, error) {
		return "https://github.com/o/r/pull/1", nil
	}

	w.runJobSync(t, 7)

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.finished) != 1 {
		t.Fatalf("finished outcomes = %d, want 1", len(store.finished))
	}
	if store.finished[0].Outcome != "done" || store.finished[0].PRURL != "https://github.com/o/r/pull/1" {
		t.Errorf("outcome = %+v, want done with PR URL", store.finished[0])
	}
}

func TestRunJobRecordsBlockedOutcome(t *testing.T) {
	ghc := &fakeGH{}
	store := newFakeStore()
	store.rows[7] = true

	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	w.RunJob = func(context.Context, int) (string, error) { return "", fmt.Errorf("%w: gap", runner.ErrBlocked) }

	w.runJobSync(t, 7)

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.finished) != 1 || store.finished[0].Outcome != "blocked" {
		t.Fatalf("outcomes = %v, want one blocked", store.finished)
	}
	if store.finished[0].Detail == "" {
		t.Error("Detail empty, want the gap text")
	}
}

// runJobSync runs runJob synchronously with a pre-acquired slot so the test
// does not need goroutine synchronization.
func (w *Watcher) runJobSync(t *testing.T, issue int) {
	t.Helper()
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	var wg sync.WaitGroup
	wg.Add(1)
	w.runJob(context.Background(), gh.Issue{Number: issue}, slots, &wg)
	wg.Wait()
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
