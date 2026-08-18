package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/prompt"
)

type fakeGit struct {
	changed  bool
	worktree string
	onAdd    func(dir string) error
	pushed   []string
	removed  []string
	deleted  []string
}

func (f *fakeGit) Origin(context.Context) (string, string, error) { return "o", "r", nil }
func (f *fakeGit) Fetch(context.Context) error                    { return nil }
func (f *fakeGit) DefaultBranch(context.Context) (string, error)  { return "main", nil }

func (f *fakeGit) AddWorktree(_ context.Context, _, dir, _ string) error {
	f.worktree = dir
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

func (f *fakeGit) HasChanges(context.Context, string, string) (bool, error) {
	return f.changed, nil
}

func (f *fakeGit) CommitAll(context.Context, string, string) error { return nil }

func (f *fakeGit) Push(_ context.Context, _, branch string) error {
	f.pushed = append(f.pushed, branch)
	return nil
}

type fakeGH struct {
	prs         []string
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

func (f *fakeGH) CreatePR(_ context.Context, _, title, _, _, _ string) (string, error) {
	if f.createPRErr != nil {
		return "", f.createPRErr
	}
	f.prs = append(f.prs, title)
	return "https://github.com/o/r/pull/1", nil
}

type fakeHarness struct{}

func (fakeHarness) Name() string { return "fake" }

func (fakeHarness) Check(context.Context) (string, error) { return "fake", nil }

func (fakeHarness) Run(context.Context, harness.Request) (harness.Result, error) {
	return harness.Result{}, nil
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
	if len(c.removed) != 1 || c.removed[0] != "7:romp" {
		t.Errorf("labels removed = %v, want [7:romp]", c.removed)
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
