package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/progress"
	"github.com/BRO3886/romp/internal/prompt"
	"github.com/BRO3886/romp/internal/review"
)

type fakeGit struct {
	changed       bool
	worktree      string
	worktreeBase  string
	changesBase   string
	defaultBranch string
	defaultErr    error
	defaultCalls  int
	refreshErr    error
	refreshCommit string
	refreshed     []string
	onAdd         func(dir string) error
	onHasChanges  func()
	pushed        []string
	removed       []string
	deleted       []string
	files         []string
	fileSequences [][]string
	diff          string
	log           string
	commits       []string
	commitCtx     *bool
	events        *[]string
}

func (f *fakeGit) Origin(context.Context) (string, string, error) { return "o", "r", nil }
func (f *fakeGit) DefaultBranch(context.Context) (string, error) {
	f.defaultCalls++
	if f.defaultErr != nil {
		return "", f.defaultErr
	}
	if f.defaultBranch != "" {
		return f.defaultBranch, nil
	}
	return "main", nil
}

func (f *fakeGit) RefreshBranch(_ context.Context, branch string) (string, error) {
	f.refreshed = append(f.refreshed, branch)
	if f.refreshErr != nil {
		return "", f.refreshErr
	}
	if f.refreshCommit != "" {
		return f.refreshCommit, nil
	}
	return "default-commit", nil
}

func (f *fakeGit) AddWorktree(_ context.Context, _, dir, base string) error {
	f.worktree = dir
	f.worktreeBase = base
	if err := os.MkdirAll(filepath.Join(dir, ".romp"), 0o755); err != nil {
		return err
	}
	if f.onAdd != nil {
		return f.onAdd(dir)
	}
	return nil
}

func (f *fakeGit) RemoveWorktree(_ context.Context, dir string) error {
	f.removed = append(f.removed, dir)
	return nil
}

func (f *fakeGit) DeleteBranch(_ context.Context, branch string) error {
	f.deleted = append(f.deleted, branch)
	return nil
}

func (f *fakeGit) HasChanges(_ context.Context, _, base string) (bool, error) {
	f.changesBase = base
	if f.onHasChanges != nil {
		f.onHasChanges()
	}
	return f.changed, nil
}

func (f *fakeGit) CommitAll(ctx context.Context, _ string, msg string) error {
	if f.commitCtx != nil {
		_, *f.commitCtx = ctx.Deadline()
	}
	f.commits = append(f.commits, msg)
	return nil
}

func (f *fakeGit) ChangedFiles(context.Context, string, string) ([]string, error) {
	if len(f.fileSequences) > 0 {
		files := f.fileSequences[0]
		f.fileSequences = f.fileSequences[1:]
		return append([]string(nil), files...), nil
	}
	return append([]string(nil), f.files...), nil
}

func (f *fakeGit) Diff(context.Context, string, string) (string, error) { return f.diff, nil }

func (f *fakeGit) BranchLog(context.Context, string, string) (string, error) { return f.log, nil }

func (f *fakeGit) Push(_ context.Context, _, branch string) error {
	f.pushed = append(f.pushed, branch)
	if f.events != nil {
		*f.events = append(*f.events, "push")
	}
	return nil
}

type fakeGH struct {
	prs          []string
	prBodies     []string
	comments     []string
	added        []string
	removed      []string
	removeErr    error
	createPRErr  error
	prComments   []string
	commentPRErr error
	events       *[]string
}

func (f *fakeGH) Issue(_ context.Context, _ string, number int) (gh.Issue, error) {
	return gh.Issue{Number: number, Title: "a title", Body: "a body"}, nil
}

func (f *fakeGH) Comment(_ context.Context, _ string, number int, body string) error {
	f.comments = append(f.comments, fmt.Sprintf("%d:%s", number, body))
	return nil
}

func (f *fakeGH) AddLabel(_ context.Context, _ string, number int, label string) error {
	f.added = append(f.added, fmt.Sprintf("%d:%s", number, label))
	return nil
}

func (f *fakeGH) RemoveLabel(_ context.Context, _ string, number int, label string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, fmt.Sprintf("%d:%s", number, label))
	return nil
}

func (f *fakeGH) CreatePR(_ context.Context, _, title, body, _, _ string) (string, error) {
	if f.events != nil {
		*f.events = append(*f.events, "create-pr")
	}
	if f.createPRErr != nil {
		return "", f.createPRErr
	}
	f.prs = append(f.prs, title)
	f.prBodies = append(f.prBodies, body)
	return "https://github.com/o/r/pull/1", nil
}

func (f *fakeGH) CommentPR(_ context.Context, _ string, _ string, body string) error {
	if f.events != nil {
		*f.events = append(*f.events, "pr-comment")
	}
	if f.commentPRErr != nil {
		return f.commentPRErr
	}
	f.prComments = append(f.prComments, body)
	return nil
}

type fakeHarness struct {
	result   harness.Result
	err      error
	calls    *int
	deadline *bool
}

func (fakeHarness) Name() string { return "fake" }

func (fakeHarness) Check(context.Context) (string, error) { return "fake", nil }

func (f fakeHarness) Run(ctx context.Context, _ harness.Request) (harness.Result, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.deadline != nil {
		_, *f.deadline = ctx.Deadline()
	}
	return f.result, f.err
}

type sequenceHarness struct {
	mu               sync.Mutex
	results          []harness.Result
	requests         []harness.Request
	delay            time.Duration
	event            string
	events           *[]string
	reviewPassEvents map[int]bool
}

func (f *sequenceHarness) Name() string                          { return "sequence" }
func (f *sequenceHarness) Check(context.Context) (string, error) { return "sequence", nil }
func (f *sequenceHarness) Run(_ context.Context, req harness.Request) (harness.Result, error) {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if req.ReadOnly {
		pass := strings.Count(req.Prompt, " verdict: ")
		if pass >= len(f.results) {
			return harness.Result{}, errors.New("unexpected harness call")
		}
		if f.reviewPassEvents == nil {
			f.reviewPassEvents = make(map[int]bool)
		}
		if !f.reviewPassEvents[pass] && f.events != nil && f.event != "" {
			*f.events = append(*f.events, f.event)
			f.reviewPassEvents[pass] = true
		}
		return f.results[pass], nil
	}
	if f.events != nil && f.event != "" {
		*f.events = append(*f.events, f.event)
	}
	if len(f.results) == 0 {
		return harness.Result{}, errors.New("unexpected harness call")
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result, nil
}

type fakeSessionStore struct {
	repo      string
	issue     int
	sessionID string
	calls     int
}

type fakeReviewStore struct {
	metrics review.Instrumentation
	calls   int
}

func (f *fakeReviewStore) SetReviewInstrumentation(_ context.Context, _ string, _ int, metrics review.Instrumentation) error {
	f.metrics = metrics
	f.calls++
	return nil
}

func (f *fakeSessionStore) SetSessionID(_ context.Context, repo string, issue int, sessionID string) error {
	f.repo = repo
	f.issue = issue
	f.sessionID = sessionID
	f.calls++
	return nil
}

// blockingHarness never finishes on its own; it returns when its context is
// cancelled, which is what the job timeout does.
type blockingHarness struct{}

func (blockingHarness) Name() string { return "blocking" }

func (blockingHarness) Check(context.Context) (string, error) { return "blocking", nil }

func (blockingHarness) Run(ctx context.Context, _ harness.Request) (harness.Result, error) {
	<-ctx.Done()
	return harness.Result{}, ctx.Err()
}

// writePR is an AddWorktree hook standing in for an agent that finished the
// work and left a PR artifact behind.
func writePR(dir string) error {
	return os.WriteFile(filepath.Join(dir, prFile),
		[]byte("---\ntitle: a title\ncommit: fix: a thing\n---\n\nthe body\n"), 0o644)
}

// writeBlocked is an AddWorktree hook standing in for an agent that stopped
// because the issue is under-scoped.
func writeBlocked(dir string) error {
	return os.WriteFile(filepath.Join(dir, blockedFile), []byte("the gap\n"), 0o644)
}

func newTestRunner(t *testing.T, g *fakeGit, c *fakeGH, verify []string) *Runner {
	t.Helper()
	// cacheDir() reads the user cache dir; keep the fake worktree out of the
	// real one.
	cache := t.TempDir()
	t.Setenv("HOME", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	return &Runner{
		Harness:      fakeHarness{},
		Git:          g,
		GH:           c,
		Prompt:       &prompt.Renderer{Template: prompt.Default()},
		Verify:       verify,
		MaxFixRounds: 2,
		Stderr:       io.Discard,
	}
}

func TestRunRemovesTriggerLabelOnSuccess(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR}
	c := &fakeGH{}
	r := newTestRunner(t, g, c, []string{"true"})

	url, err := r.Run(context.Background(), 7)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if url != "https://github.com/o/r/pull/1" {
		t.Errorf("Run URL = %q, want the PR URL", url)
	}
	if len(c.prs) != 1 {
		t.Fatalf("PRs opened = %v, want one", c.prs)
	}
	if got, want := c.prBodies, []string{"the body\n\nCloses #7\n\nCreated with [romp](https://romp.sidv.dev) 🦦"}; !slices.Equal(got, want) {
		t.Errorf("PR bodies = %v, want %v", got, want)
	}
	if len(c.removed) != 1 || c.removed[0] != "7:romp" {
		t.Errorf("labels removed = %v, want [7:romp]", c.removed)
	}
	if g.worktreeBase != "default-commit" {
		t.Errorf("worktree base = %q, want exact synced commit", g.worktreeBase)
	}
	if g.changesBase != "default-commit" {
		t.Errorf("changes base = %q, want exact synced commit", g.changesBase)
	}
	if g.defaultCalls != 1 || !slices.Equal(g.refreshed, []string{"main"}) {
		t.Errorf("base selection calls = default:%d refresh:%v, want default:1 refresh:[main]", g.defaultCalls, g.refreshed)
	}
}

func TestRunConfiguredBaseSkipsDefaultBranch(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, refreshCommit: "stable-sha"}
	r := newTestRunner(t, g, &fakeGH{}, []string{"true"})
	r.Base = "stable"

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if g.defaultCalls != 0 || !slices.Equal(g.refreshed, []string{"stable"}) {
		t.Errorf("base selection calls = default:%d refresh:%v, want default:0 refresh:[stable]", g.defaultCalls, g.refreshed)
	}
	if g.worktreeBase != "stable-sha" || g.changesBase != "stable-sha" {
		t.Errorf("exact base = worktree:%q changes:%q, want stable-sha", g.worktreeBase, g.changesBase)
	}
}

func TestRunStopsBeforeHarnessWhenBaseResolutionFails(t *testing.T) {
	tests := []struct {
		name       string
		defaultErr error
		refreshErr error
		want       string
	}{
		{name: "refresh", refreshErr: errors.New("refreshing origin/main: unavailable"), want: "refresh base"},
		{name: "default branch", defaultErr: errors.New("remote HEAD does not identify a default branch"), want: "default branch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			g := &fakeGit{defaultErr: tt.defaultErr, refreshErr: tt.refreshErr}
			r := newTestRunner(t, g, &fakeGH{}, []string{"true"})
			r.Harness = fakeHarness{calls: &calls}

			_, err := r.Run(context.Background(), 7)
			cause := tt.defaultErr
			if cause == nil {
				cause = tt.refreshErr
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), cause.Error()) {
				t.Fatalf("Run error = %v, want actionable base refresh error", err)
			}
			if calls != 0 {
				t.Errorf("harness calls = %d, want none", calls)
			}
			if g.worktree != "" {
				t.Errorf("worktree = %q, want none", g.worktree)
			}
		})
	}
}

func TestRunRecordsAndLogsSessionAfterHarnessSuccess(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR}
	c := &fakeGH{}
	sessions := &fakeSessionStore{}
	var logs strings.Builder
	r := newTestRunner(t, g, c, []string{"true"})
	r.Harness = fakeHarness{result: harness.Result{Output: "done", SessionID: "session-7"}}
	r.Sessions = sessions
	r.Codename = "sunny_naruto"
	r.Stderr = &logs

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sessions.calls != 1 || sessions.repo != "o/r" || sessions.issue != 7 || sessions.sessionID != "session-7" {
		t.Errorf("session write = %+v", sessions)
	}
	if !strings.Contains(logs.String(), "[sunny_naruto] session session-7") {
		t.Errorf("logs missing session line:\n%s", logs.String())
	}
}

func TestRunKeepsSuccessfulVerificationOutputOutOfLogs(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR}
	var logs strings.Builder
	t.Setenv("ROMP_TEST_VERIFY_OUTPUT", "large-build-output")
	r := newTestRunner(t, g, &fakeGH{}, []string{`printf "$ROMP_TEST_VERIFY_OUTPUT"`})
	r.Stderr = &logs

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(logs.String(), "large-build-output") {
		t.Fatalf("successful command output leaked into logs:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "verify ok (printf \"$ROMP_TEST_VERIFY_OUTPUT\")") {
		t.Fatalf("compact verification result missing from logs:\n%s", logs.String())
	}
}

func TestRunEmitsDashboardPhases(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR}
	r := newTestRunner(t, g, &fakeGH{}, []string{"true"})
	var phases []progress.Phase
	r.Progress = func(event progress.Event) {
		if event.Issue != 7 {
			t.Errorf("event issue = %d, want 7", event.Issue)
		}
		phases = append(phases, event.Phase)
	}

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []progress.Phase{
		progress.PhasePreparing,
		progress.PhaseAgent,
		progress.PhaseVerifying,
		progress.PhasePublishing,
	}
	if !slices.Equal(phases, want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
}

func TestRunDoesNotRecordSessionWhenHarnessFails(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR}
	c := &fakeGH{}
	sessions := &fakeSessionStore{}
	var logs strings.Builder
	r := newTestRunner(t, g, c, []string{"true"})
	r.Harness = fakeHarness{
		result: harness.Result{SessionID: "session-from-failed-cli"},
		err:    errors.New("CLI failed: stdout and stderr"),
	}
	r.Sessions = sessions
	r.Stderr = &logs

	if _, err := r.Run(context.Background(), 7); err == nil {
		t.Fatal("Run error = nil, want harness failure")
	}
	if sessions.calls != 0 {
		t.Errorf("session writes = %d, want none", sessions.calls)
	}
	if strings.Contains(logs.String(), "session session-from-failed-cli") {
		t.Errorf("failed session was logged:\n%s", logs.String())
	}
}

func TestHarnessFailureLeavesPersistedSessionEmptyThroughFinish(t *testing.T) {
	store, err := job.Open(filepath.Join(t.TempDir(), "romp.db"))
	if err != nil {
		t.Fatalf("job.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	if ok, err := store.Claim(ctx, "o/r", 7, "romp-7"); err != nil || !ok {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	r := newTestRunner(t, &fakeGit{changed: true, onAdd: writePR}, &fakeGH{}, []string{"true"})
	r.Harness = fakeHarness{
		result: harness.Result{SessionID: "session-from-failed-cli"},
		err:    errors.New("CLI failed with useful diagnostics"),
	}
	r.Sessions = store
	if _, err := r.Run(ctx, 7); err == nil {
		t.Fatal("Run error = nil, want harness failure")
	}
	jobs, err := store.List(ctx, "o/r")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 || jobs[0].SessionID != "" {
		t.Fatalf("active jobs after failure = %+v", jobs)
	}
	if err := store.Finish(ctx, job.Outcome{
		Repo: "o/r", Issue: 7, Outcome: "error", Branch: "romp-7", FinishedAt: "2026-08-18T12:00:00Z",
	}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	outcomes, err := store.History(ctx, "o/r", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].SessionID != "" || outcomes[0].Outcome != "error" {
		t.Fatalf("outcomes after failure = %+v", outcomes)
	}
}

func TestRunExposesSessionOnActiveJobBeforeFinish(t *testing.T) {
	store, err := job.Open(filepath.Join(t.TempDir(), "romp.db"))
	if err != nil {
		t.Fatalf("job.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if ok, err := store.Claim(context.Background(), "o/r", 7, "romp-7"); err != nil || !ok {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	reachedDownstream := make(chan struct{})
	continueRun := make(chan struct{})
	g := &fakeGit{
		changed: true,
		onAdd:   writePR,
		onHasChanges: func() {
			close(reachedDownstream)
			<-continueRun
		},
	}
	r := newTestRunner(t, g, &fakeGH{}, []string{"true"})
	r.Harness = fakeHarness{result: harness.Result{SessionID: "active-session"}}
	r.Sessions = store

	runErr := make(chan error, 1)
	go func() {
		_, err := r.Run(context.Background(), 7)
		runErr <- err
	}()
	select {
	case <-reachedDownstream:
	case <-time.After(2 * time.Second):
		close(continueRun)
		t.Fatal("runner did not reach the downstream pause")
	}
	jobs, err := store.List(context.Background(), "o/r")
	if err != nil {
		close(continueRun)
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 || jobs[0].SessionID != "active-session" {
		close(continueRun)
		t.Fatalf("active jobs = %+v", jobs)
	}
	close(continueRun)
	if err := <-runErr; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestVerifyRunsCommandsThroughShell(t *testing.T) {
	r := &Runner{
		Verify: []string{`test "$(printf '%s' 'hello world')" = "hello world"`},
		Stderr: io.Discard,
	}

	if err := r.verify(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestRunRemovesConfiguredTriggerLabel(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR}
	c := &fakeGH{}
	r := newTestRunner(t, g, c, []string{"true"})
	r.TriggerLabel = "agent"

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(c.removed) != 1 || c.removed[0] != "7:agent" {
		t.Errorf("labels removed = %v, want [7:agent]", c.removed)
	}
}

func TestRunFailsWhenTriggerLabelRemovalFails(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR}
	c := &fakeGH{removeErr: errors.New("boom")}
	r := newTestRunner(t, g, c, []string{"true"})

	_, err := r.Run(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "removing romp label") {
		t.Fatalf("Run error = %v, want a trigger-label removal failure", err)
	}
}

func TestRunTimeout(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR}
	c := &fakeGH{}
	r := newTestRunner(t, g, c, []string{"true"})
	r.Harness = blockingHarness{}
	r.Timeout = 50 * time.Millisecond

	_, err := r.Run(context.Background(), 7)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Run error = %v, want ErrTimeout", err)
	}
	if len(c.prs) != 0 {
		t.Errorf("PRs opened = %v, want none", c.prs)
	}
}

func TestRunWithoutTimeoutDoesNotSetDeadline(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR}
	c := &fakeGH{}
	r := newTestRunner(t, g, c, []string{"true"})
	hasDeadline := true
	r.Harness = fakeHarness{deadline: &hasDeadline}

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hasDeadline {
		t.Error("harness context has a deadline, want unbounded")
	}
}

func TestRunKeepsTriggerLabelOnFailure(t *testing.T) {
	tests := []struct {
		name    string
		git     *fakeGit
		verify  []string
		wantErr string
	}{
		{
			name:    "blocked",
			git:     &fakeGit{changed: true, onAdd: writeBlocked},
			verify:  []string{"true"},
			wantErr: "blocked",
		},
		{
			name:    "no changes",
			git:     &fakeGit{changed: false, onAdd: writePR},
			verify:  []string{"true"},
			wantErr: "no-changes",
		},
		{
			name:    "red",
			git:     &fakeGit{changed: true, onAdd: writePR},
			verify:  []string{"false"},
			wantErr: "red",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &fakeGH{}
			r := newTestRunner(t, tt.git, c, tt.verify)

			_, err := r.Run(context.Background(), 7)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run error = %v, want one containing %q", err, tt.wantErr)
			}
			if len(c.prs) != 0 {
				t.Errorf("PRs opened = %v, want none", c.prs)
			}
			if len(c.removed) != 0 {
				t.Errorf("labels removed = %v, want none", c.removed)
			}
		})
	}
}

func TestRunReviewGatePaths(t *testing.T) {
	approve := `{"verdict":"approve","findings":[]}`
	approveWithNit := `{"verdict":"approve","findings":[{"severity":"nit","file":"internal/a.go","line":4,"description":"Use the existing helper."}]}`
	fix := `{"verdict":"fix","findings":[{"severity":"blocking","file":"internal/a.go","line":7,"description":"The error path is lost."}]}`
	fixAgain := `{"verdict":"fix","findings":[{"severity":"blocking","file":"internal/b.go","line":9,"description":"The retry path is lost."}]}`
	tests := []struct {
		name          string
		files         []string
		reviews       []harness.Result
		wantBuilder   int
		wantReviewer  int
		wantErr       error
		wantNit       bool
		wantFixPrompt bool
		wantLabel     bool
		wantComments  int
	}{
		{name: "approve first", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: approve}}, wantBuilder: 1, wantReviewer: 1, wantComments: 1},
		{name: "approve with nits", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: approveWithNit}}, wantBuilder: 1, wantReviewer: 1, wantNit: true, wantComments: 1},
		{name: "fix then approve", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: fix}, {Output: approve}}, wantBuilder: 2, wantReviewer: 2, wantFixPrompt: true, wantComments: 2},
		{name: "fix twice then approve", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: fix}, {Output: fixAgain}, {Output: approve}}, wantBuilder: 3, wantReviewer: 3, wantFixPrompt: true, wantComments: 3},
		{name: "fix rounds exhausted", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: fix}, {Output: fix}, {Output: fix}}, wantBuilder: 3, wantReviewer: 3, wantErr: ErrChangesRequested, wantFixPrompt: true, wantLabel: true, wantComments: 3},
		{name: "malformed", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: "not json"}}, wantBuilder: 1, wantReviewer: 1, wantComments: 1},
		{name: "docs only", files: []string{"docs/readme.md"}, wantBuilder: 1, wantReviewer: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &fakeGit{changed: true, onAdd: writePR, files: tt.files, diff: "diff --git a/a b/a", log: "abc fix: a thing"}
			c := &fakeGH{}
			builder := &sequenceHarness{results: make([]harness.Result, tt.wantBuilder), delay: 2 * time.Millisecond}
			reviewer := &sequenceHarness{results: append([]harness.Result(nil), tt.reviews...), delay: 2 * time.Millisecond}
			r := newTestRunner(t, g, c, []string{"printf verified"})
			r.Harness = builder
			r.ReviewHarness = reviewer
			r.ReviewEnabled = true
			instrumentation := &fakeReviewStore{}
			r.ReviewInstrumentation = instrumentation
			var logs strings.Builder
			r.Stderr = &logs

			url, err := r.Run(context.Background(), 7)
			if tt.name == "malformed" {
				if err == nil || !strings.Contains(err.Error(), "parse review outcome") {
					t.Fatalf("Run error = %v, want malformed review error", err)
				}
			} else if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Run error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Run: %v", err)
			}
			wantReviewCalls := 0
			if tt.wantReviewer > 0 {
				wantReviewCalls = tt.wantReviewer * len(review.BuildPlan(tt.files, false).Lenses)
			}
			if len(builder.requests) != tt.wantBuilder || len(reviewer.requests) != wantReviewCalls {
				t.Fatalf("calls = builder:%d reviewer:%d, want %d/%d", len(builder.requests), len(reviewer.requests), tt.wantBuilder, wantReviewCalls)
			}
			if len(c.prs) != 1 {
				t.Errorf("PRs opened = %v, want one before review handling", c.prs)
			}
			if url != "https://github.com/o/r/pull/1" {
				t.Errorf("Run URL = %q, want the open PR URL", url)
			}
			for _, req := range reviewer.requests {
				if !req.ReadOnly || !strings.Contains(req.Prompt, "printf verified") || !strings.Contains(req.Prompt, "diff --git") {
					t.Errorf("review request missing read-only contract inputs: %+v", req)
				}
			}
			if tt.wantFixPrompt && !strings.Contains(builder.requests[1].Prompt, "The error path is lost.") {
				t.Errorf("fix prompt missing blocking finding:\n%s", builder.requests[1].Prompt)
			}
			if tt.name == "fix twice then approve" && !strings.Contains(builder.requests[2].Prompt, "The retry path is lost.") {
				t.Errorf("second fix prompt missing current blocking finding:\n%s", builder.requests[2].Prompt)
			}
			if tt.wantFixPrompt {
				want := []string{"fix: a thing"}
				for range tt.wantBuilder - 1 {
					want = append(want, "fix: address review findings for #7")
				}
				if !slices.Equal(g.commits, want) {
					t.Errorf("commit subjects = %v, want %v", g.commits, want)
				}
			}
			if len(c.prComments) != tt.wantComments {
				t.Errorf("PR comments = %v, want %d", c.prComments, tt.wantComments)
			}
			if tt.wantNit && (len(c.prComments) != 1 || !strings.Contains(c.prComments[0], "### Nit") || !strings.Contains(c.prComments[0], "internal/a.go:4: Use the existing helper.")) {
				t.Errorf("PR comments = %v, want located nit", c.prComments)
			}
			if tt.wantLabel && !slices.Contains(c.added, "7:romp:changes-requested") {
				t.Errorf("labels added = %v", c.added)
			}
			if len(c.comments) != 0 {
				t.Errorf("issue comments = %v, want review records only on the PR", c.comments)
			}
			if instrumentation.calls == 0 {
				t.Fatal("review instrumentation was not recorded")
			}
			if instrumentation.metrics.BuilderDurationMS < int64(tt.wantBuilder) {
				t.Errorf("BuilderDurationMS = %d, want a measured duration for %d runs", instrumentation.metrics.BuilderDurationMS, tt.wantBuilder)
			}
			if tt.name == "docs only" {
				if instrumentation.metrics.ReviewRan || instrumentation.metrics.SkipReason != review.SkipDocsOnly {
					t.Errorf("docs-only instrumentation = %+v", instrumentation.metrics)
				}
				if strings.Contains(logs.String(), "review: running") {
					t.Errorf("docs-only job claimed reviewer started:\n%s", logs.String())
				}
			} else {
				if !strings.Contains(logs.String(), "review: running sequence across") {
					t.Errorf("logs missing reviewer start:\n%s", logs.String())
				}
				if !instrumentation.metrics.ReviewRan || len(instrumentation.metrics.Passes) != tt.wantReviewer {
					t.Errorf("review instrumentation = %+v, want %d passes", instrumentation.metrics, tt.wantReviewer)
				}
				for passNumber, pass := range instrumentation.metrics.Passes {
					if pass.DurationMS < 1 {
						t.Errorf("review pass %d duration = %dms, want measured harness time", passNumber+1, pass.DurationMS)
					}
					if pass.LensCount != len(review.BuildPlan(tt.files, false).Lenses) {
						t.Errorf("review pass %d lens count = %d, want %d", passNumber+1, pass.LensCount, len(review.BuildPlan(tt.files, false).Lenses))
					}
				}
				if tt.name != "malformed" && tt.wantFixPrompt != instrumentation.metrics.FixRoundFired {
					t.Errorf("FixRoundFired = %t, want %t", instrumentation.metrics.FixRoundFired, tt.wantFixPrompt)
				}
				if !strings.Contains(logs.String(), "review pass 1:") {
					t.Errorf("logs missing live review summary:\n%s", logs.String())
				}
				if tt.name == "approve with nits" && instrumentation.metrics.Passes[0].Nit != 1 {
					t.Errorf("approve-with-nits pass = %+v", instrumentation.metrics.Passes[0])
				}
			}
		})
	}
}

func TestRunReviewGateHonorsZeroFixRoundBudget(t *testing.T) {
	fix := `{"verdict":"fix","findings":[{"severity":"blocking","file":"internal/a.go","line":7,"description":"The error path is lost."}]}`
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}, diff: "diff --git a/a b/a", log: "abc fix: a thing"}
	c := &fakeGH{}
	builder := &sequenceHarness{results: []harness.Result{{}}}
	reviewer := &sequenceHarness{results: []harness.Result{{Output: fix}}}
	r := newTestRunner(t, g, c, []string{"true"})
	r.Harness = builder
	r.ReviewHarness = reviewer
	r.ReviewEnabled = true
	r.MaxFixRounds = 0

	_, err := r.Run(context.Background(), 7)
	if !errors.Is(err, ErrChangesRequested) {
		t.Fatalf("Run error = %v, want %v", err, ErrChangesRequested)
	}
	wantReviewCalls := len(review.BuildPlan([]string{"internal/a.go"}, false).Lenses)
	if len(builder.requests) != 1 || len(reviewer.requests) != wantReviewCalls {
		t.Fatalf("calls = builder:%d reviewer:%d, want 1/%d", len(builder.requests), len(reviewer.requests), wantReviewCalls)
	}
	if len(c.prComments) != 1 || !slices.Contains(c.added, "7:romp:changes-requested") {
		t.Errorf("comments/labels = %d/%v, want one review comment and changes-requested label", len(c.prComments), c.added)
	}
}

func TestRunRecordsDisabledReview(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR}
	c := &fakeGH{}
	r := newTestRunner(t, g, c, []string{"true"})
	instrumentation := &fakeReviewStore{}
	r.ReviewInstrumentation = instrumentation

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if instrumentation.calls != 1 || instrumentation.metrics.ReviewRan || instrumentation.metrics.SkipReason != review.SkipDisabled {
		t.Errorf("disabled review instrumentation = %+v, calls %d", instrumentation.metrics, instrumentation.calls)
	}
	if len(c.prs) != 1 || len(c.prComments) != 0 {
		t.Errorf("disabled review PRs/comments = %d/%d, want 1/0", len(c.prs), len(c.prComments))
	}
}

func TestRunNoChangesCleansWorktreeAndBranch(t *testing.T) {
	g := &fakeGit{changed: false, onAdd: writePR}
	r := newTestRunner(t, g, &fakeGH{}, []string{"true"})

	_, err := r.Run(context.Background(), 7)
	if !errors.Is(err, ErrNoChanges) {
		t.Fatalf("Run error = %v, want ErrNoChanges", err)
	}
	if len(g.removed) != 1 || !slices.Equal(g.deleted, []string{"romp-7"}) {
		t.Errorf("cleanup = worktrees:%v branches:%v, want one worktree and romp-7", g.removed, g.deleted)
	}
}

func TestRunTimeoutDuringReviewUsesJobTimeoutOutcome(t *testing.T) {
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}}
	r := newTestRunner(t, g, &fakeGH{}, []string{"true"})
	r.ReviewEnabled = true
	r.ReviewHarness = blockingHarness{}
	r.Timeout = 50 * time.Millisecond

	_, err := r.Run(context.Background(), 7)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Run error = %v, want ErrTimeout", err)
	}
}

func TestRunCommitUsesJobTimeoutContext(t *testing.T) {
	hasDeadline := false
	g := &fakeGit{changed: true, onAdd: writePR, commitCtx: &hasDeadline}
	r := newTestRunner(t, g, &fakeGH{}, []string{"true"})
	r.Timeout = time.Minute

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDeadline {
		t.Error("commit context has no deadline, want the job timeout deadline")
	}
}
