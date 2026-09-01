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
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/prompt"
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
	diff          string
	log           string
	commits       int
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

func (f *fakeGit) CommitAll(context.Context, string, string) error { return nil }

func (f *fakeGit) ChangedFiles(context.Context, string, string) ([]string, error) {
	return append([]string(nil), f.files...), nil
}

func (f *fakeGit) Diff(context.Context, string, string) (string, error) { return f.diff, nil }

func (f *fakeGit) BranchLog(context.Context, string, string) (string, error) { return f.log, nil }

func (f *fakeGit) Push(_ context.Context, _, branch string) error {
	f.pushed = append(f.pushed, branch)
	return nil
}

type fakeGH struct {
	prs         []string
	prBodies    []string
	comments    []string
	added       []string
	removed     []string
	removeErr   error
	createPRErr error
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
	if f.createPRErr != nil {
		return "", f.createPRErr
	}
	f.prs = append(f.prs, title)
	f.prBodies = append(f.prBodies, body)
	return "https://github.com/o/r/pull/1", nil
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
	results  []harness.Result
	requests []harness.Request
}

func (f *sequenceHarness) Name() string                          { return "sequence" }
func (f *sequenceHarness) Check(context.Context) (string, error) { return "sequence", nil }
func (f *sequenceHarness) Run(_ context.Context, req harness.Request) (harness.Result, error) {
	f.requests = append(f.requests, req)
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
		Harness: fakeHarness{},
		Git:     g,
		GH:      c,
		Prompt:  &prompt.Renderer{Template: prompt.Default()},
		Verify:  verify,
		Stderr:  io.Discard,
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
	tests := []struct {
		name          string
		files         []string
		reviews       []harness.Result
		wantBuilder   int
		wantReviewer  int
		wantErr       error
		wantNotes     bool
		wantFixPrompt bool
		wantLabel     bool
		wantComment   bool
	}{
		{name: "approve first", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: approve}}, wantBuilder: 1, wantReviewer: 1},
		{name: "approve with nits", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: approveWithNit}}, wantBuilder: 1, wantReviewer: 1, wantNotes: true},
		{name: "fix then approve", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: fix}, {Output: approve}}, wantBuilder: 2, wantReviewer: 2, wantFixPrompt: true},
		{name: "fix still blocking", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: fix}, {Output: fix}}, wantBuilder: 2, wantReviewer: 2, wantErr: ErrChangesRequested, wantFixPrompt: true, wantLabel: true, wantComment: true},
		{name: "malformed", files: []string{"internal/a.go"}, reviews: []harness.Result{{Output: "not json"}}, wantBuilder: 1, wantReviewer: 1},
		{name: "docs only", files: []string{"docs/readme.md"}, wantBuilder: 1, wantReviewer: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &fakeGit{changed: true, onAdd: writePR, files: tt.files, diff: "diff --git a/a b/a", log: "abc fix: a thing"}
			c := &fakeGH{}
			builder := &sequenceHarness{results: make([]harness.Result, tt.wantBuilder)}
			reviewer := &sequenceHarness{results: append([]harness.Result(nil), tt.reviews...)}
			r := newTestRunner(t, g, c, []string{"printf verified"})
			r.Harness = builder
			r.ReviewHarness = reviewer
			r.ReviewEnabled = true

			_, err := r.Run(context.Background(), 7)
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
			if len(builder.requests) != tt.wantBuilder || len(reviewer.requests) != tt.wantReviewer {
				t.Fatalf("calls = builder:%d reviewer:%d, want %d/%d", len(builder.requests), len(reviewer.requests), tt.wantBuilder, tt.wantReviewer)
			}
			for _, req := range reviewer.requests {
				if !req.ReadOnly || !strings.Contains(req.Prompt, "printf verified") || !strings.Contains(req.Prompt, "diff --git") {
					t.Errorf("review request missing read-only contract inputs: %+v", req)
				}
			}
			if tt.wantFixPrompt && !strings.Contains(builder.requests[1].Prompt, "The error path is lost.") {
				t.Errorf("fix prompt missing blocking finding:\n%s", builder.requests[1].Prompt)
			}
			if tt.wantNotes && (len(c.prBodies) != 1 || !strings.Contains(c.prBodies[0], "Reviewer notes") || !strings.Contains(c.prBodies[0], "Use the existing helper.")) {
				t.Errorf("PR bodies = %v, want reviewer notes", c.prBodies)
			}
			if tt.wantLabel && !slices.Contains(c.added, "7:romp:changes-requested") {
				t.Errorf("labels added = %v", c.added)
			}
			if tt.wantComment && len(c.comments) != 1 {
				t.Errorf("comments = %v, want blocking findings comment", c.comments)
			}
		})
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
