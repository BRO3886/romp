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
	mu         sync.Mutex
	issues     []gh.Issue
	added      []string
	removed    []string
	assigned   []int
	unassigned []int
	comments   []string
	prByBranch map[string]int
	addErr     error
	assignErr  error
}

func (f *fakeGH) ListIssues(context.Context, string, string) ([]gh.Issue, error) {
	return f.issues, nil
}

func (f *fakeGH) OpenPR(_ context.Context, _ string, branch string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prByBranch[branch], nil
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

func (f *fakeGH) Assign(_ context.Context, _ string, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.assignErr != nil {
		return f.assignErr
	}
	f.assigned = append(f.assigned, number)
	return nil
}

func (f *fakeGH) Unassign(_ context.Context, _ string, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unassigned = append(f.unassigned, number)
	return nil
}

func (f *fakeGH) RemoveLabel(_ context.Context, _ string, number int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, fmt.Sprintf("%d:%s", number, label))
	return nil
}

func (f *fakeGH) Comment(_ context.Context, _ string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, fmt.Sprintf("%d:%s", number, body))
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
	slots := make(chan struct{}, 10)
	for slot := 0; slot < cap(slots); slot++ {
		slots <- struct{}{}
	}
	if err := w.claimBatch(context.Background(), slots, &wg, context.Background()); err != nil {
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

func TestClaimBatchReconcilesOpenPR(t *testing.T) {
	ghc := &fakeGH{
		issues:     []gh.Issue{{Number: 1}, {Number: 2}},
		prByBranch: map[string]int{"romp-2": 42},
	}
	store := newFakeStore()

	w := &Watcher{
		Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", Blocked: "romp:blocked",
		Width: 10, GH: ghc, Store: store,
	}
	var ran []int
	w.RunJob = func(_ context.Context, issue int) (string, error) {
		ran = append(ran, issue)
		return "", nil
	}

	var wg sync.WaitGroup
	slots := make(chan struct{}, 10)
	for slot := 0; slot < cap(slots); slot++ {
		slots <- struct{}{}
	}
	if err := w.claimBatch(context.Background(), slots, &wg, context.Background()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	if len(ran) != 1 || ran[0] != 1 {
		t.Errorf("RunJob called for %v, want only [1]", ran)
	}
	added, removed := ghc.snapshot()
	if contains(added, "2:romp:claimed") {
		t.Errorf("issue 2 claimed despite open PR: %v", added)
	}
	if !contains(removed, "2:romp") {
		t.Errorf("trigger label not removed for reconciled issue: %v", removed)
	}
	if len(ghc.comments) != 1 || !strings.Contains(ghc.comments[0], "42") {
		t.Errorf("reconcile comment = %v, want one mentioning PR 42", ghc.comments)
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
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	if err := w.claimBatch(context.Background(), slots, &wg, context.Background()); err != nil {
		t.Fatal(err)
	}

	added, _ := ghc.snapshot()
	if len(added) != 1 {
		t.Errorf("claimed %d issues at width 1, want 1", len(added))
	}
}

func TestClaimAssignsAuthenticatedUser(t *testing.T) {
	ghc := &fakeGH{}
	store := newFakeStore()
	w := &Watcher{Repo: "o/r", Claim: "romp:claimed", GH: ghc, Store: store}

	if !w.claim(context.Background(), gh.Issue{Number: 7}) {
		t.Fatal("claim = false, want true")
	}
	if len(ghc.assigned) != 1 || ghc.assigned[0] != 7 {
		t.Errorf("assigned = %v, want [7]", ghc.assigned)
	}
}

func TestClaimRollsBackOnAssignFailure(t *testing.T) {
	ghc := &fakeGH{assignErr: errors.New("boom")}
	store := newFakeStore()
	w := &Watcher{Repo: "o/r", Claim: "romp:claimed", GH: ghc, Store: store}

	if w.claim(context.Background(), gh.Issue{Number: 7}) {
		t.Fatal("claim = true, want false when Assign fails")
	}
	if store.has(7) {
		t.Fatal("row not rolled back after Assign failure")
	}
	_, removed := ghc.snapshot()
	if len(removed) != 1 || removed[0] != "7:romp:claimed" {
		t.Errorf("claim label not removed after Assign failure: %v", removed)
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

func TestRunJobKeepsAssigneeOnDone(t *testing.T) {
	ghc := &fakeGH{}
	store := newFakeStore()
	store.rows[7] = true

	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	w.RunJob = func(context.Context, int) (string, error) {
		return "https://github.com/o/r/pull/1", nil
	}

	w.runJobSync(t, 7)

	if store.has(7) {
		t.Error("in-flight row not deleted")
	}
	_, removed := ghc.snapshot()
	if !contains(removed, "7:romp:claimed") {
		t.Errorf("claim label not released: %v", removed)
	}
	if len(ghc.unassigned) != 0 {
		t.Errorf("unassigned = %v, want none after successful PR", ghc.unassigned)
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
	if len(ghc.unassigned) != 1 || ghc.unassigned[0] != 7 {
		t.Errorf("unassigned on blocked = %v, want [7]", ghc.unassigned)
	}
}

func TestRunJobLogsTimeout(t *testing.T) {
	ghc := &fakeGH{}
	store := newFakeStore()
	store.rows[7] = true

	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	var logMsg string
	w.Logf = func(format string, a ...any) { logMsg = fmt.Sprintf(format, a...) }
	w.RunJob = func(context.Context, int) (string, error) {
		return "", fmt.Errorf("%w: killed", runner.ErrTimeout)
	}

	w.runJobSync(t, 7)

	if !strings.Contains(logMsg, "timeout") {
		t.Errorf("log = %q, want timeout", logMsg)
	}
	_, removed := ghc.snapshot()
	if !contains(removed, "7:romp:claimed") {
		t.Errorf("claim label not released on timeout: %v", removed)
	}
	if !contains(removed, "7:romp") {
		t.Errorf("trigger label not removed on timeout: %v", removed)
	}
}

func TestRunJobRemovesTriggerOnRed(t *testing.T) {
	ghc := &fakeGH{}
	store := newFakeStore()
	store.rows[7] = true

	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	w.RunJob = func(context.Context, int) (string, error) {
		return "", fmt.Errorf("%w: verify failed", runner.ErrRed)
	}

	w.runJobSync(t, 7)

	_, removed := ghc.snapshot()
	if !contains(removed, "7:romp:claimed") || !contains(removed, "7:romp") {
		t.Errorf("removed labels = %v, want claim and trigger labels", removed)
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

func TestRunJobRecordsChangesRequestedOutcome(t *testing.T) {
	ghc := &fakeGH{}
	store := newFakeStore()
	store.rows[7] = true

	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", GH: ghc, Store: store}
	w.RunJob = func(context.Context, int) (string, error) {
		return "", fmt.Errorf("%w: blocking finding", runner.ErrChangesRequested)
	}

	w.runJobSync(t, 7)

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.finished) != 1 || store.finished[0].Outcome != "changes-requested" {
		t.Fatalf("outcomes = %v, want one changes-requested", store.finished)
	}
}

func TestColorizerMapsWatchTokens(t *testing.T) {
	c := newColorizer(true, "sunny_naruto")
	got := c.colorize("12:34:56  [sunny_naruto] warning: PR: https://github.com/o/r/pull/1 #1: done #2: blocked #3: timeout #4: red\n")
	want := "\x1b[2m12:34:56\x1b[0m  \x1b[36m[sunny_naruto]\x1b[0m \x1b[33mwarning\x1b[0m: PR: \x1b[33mhttps://github.com/o/r/pull/1\x1b[0m #1: \x1b[1;32mdone\x1b[0m #2: \x1b[1;33mblocked\x1b[0m #3: \x1b[1;31mtimeout\x1b[0m #4: \x1b[1;31mred\x1b[0m\n"
	if got != want {
		t.Errorf("colorize = %q, want %q", got, want)
	}
}

func TestColorizerUsesDeterministicNameColors(t *testing.T) {
	name := "sunny_naruto"
	got := newColorizer(true, name).colorize("12:34:56  [" + name + "] running codex\n")
	want := newColorizer(true, name).colorize("12:34:56  [" + name + "] running codex\n")
	if got != want || !strings.Contains(got, "\x1b[") {
		t.Errorf("colorize(%q) = %q, want deterministic colored output", name, got)
	}
}

func TestColorizerDisablesColorWhenNotInteractive(t *testing.T) {
	line := "12:34:56  [sunny_naruto] done\n"
	if got := newColorizer(false, "sunny_naruto").colorize(line); got != line {
		t.Errorf("colorize = %q, want unmodified line %q", got, line)
	}
	if colorEnabled(false) {
		t.Fatal("colorEnabled = true, want false for non-interactive stderr")
	}
}

func TestColorEnabledRespectsNOColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if colorEnabled(true) {
		t.Fatal("colorEnabled = true, want false when NO_COLOR is set")
	}
}

// runJobSync runs runJob synchronously so the test does not need goroutine synchronization.
func (w *Watcher) runJobSync(t *testing.T, issue int) {
	t.Helper()
	slots := make(chan struct{}, 1)
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
