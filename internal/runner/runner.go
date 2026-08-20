// Package runner orchestrates a single romp job: fetch the issue, build a
// worktree, run the harness, independently verify, and open a PR.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/prompt"
)

// defaultTriggerLabel is the label romp watch polls for, and the one a
// finished run removes as its completion marker.
const defaultTriggerLabel = "romp"

// ErrTimeout is returned when the job timeout expires while the harness is
// running. The agent is killed and the job stays labelled so it can retry.
var ErrTimeout = errors.New("timeout")

// ErrNoChanges is returned when the agent finished without touching the repo.
var ErrNoChanges = errors.New("no-changes")

// ErrRed is returned when a verification command fails after the agent finished.
var ErrRed = errors.New("red")

// GitOps is the git surface a run needs: read the remote and the default
// branch, manage the job worktree and branch, and publish the result.
type GitOps interface {
	Origin(ctx context.Context) (owner, name string, err error)
	Fetch(ctx context.Context) error
	DefaultBranch(ctx context.Context) (string, error)
	AddWorktree(ctx context.Context, branch, dir, base string) error
	RemoveWorktree(ctx context.Context, dir string) error
	DeleteBranch(ctx context.Context, branch string) error
	HasChanges(ctx context.Context, dir, base string) (bool, error)
	CommitAll(ctx context.Context, dir, msg string) error
	Push(ctx context.Context, dir, branch string) error
}

// GHOps is the GitHub surface a run needs: read the issue, report a block,
// move labels, and open the PR.
type GHOps interface {
	Issue(ctx context.Context, repo string, number int) (gh.Issue, error)
	Comment(ctx context.Context, repo string, number int, body string) error
	AddLabel(ctx context.Context, repo string, number int, label string) error
	RemoveLabel(ctx context.Context, repo string, number int, label string) error
	CreatePR(ctx context.Context, repo, title, body, head, base string) (string, error)
}

// Runner wires the ports together for a single one-shot job.
type Runner struct {
	Harness      harness.Harness
	Git          GitOps
	GH           GHOps
	Prompt       *prompt.Renderer
	Verify       []string
	Model        string
	Effort       string
	MaxTurns     int
	Codename     string
	Base         string
	Timeout      time.Duration
	Protected    []string
	Ignore       []string
	Brief        string
	TriggerLabel string
	BlockedLabel string
	Stderr       io.Writer
}

func (r *Runner) logf(format string, a ...any) {
	prefix := ""
	if r.Codename != "" {
		prefix = "[" + r.Codename + "] "
	}
	fmt.Fprintf(r.Stderr, "%s  %s%s\n", time.Now().Format("15:04:05"), prefix, fmt.Sprintf(format, a...))
}

// triggerLabel returns the configured trigger label, falling back to the
// default when unset so a hand-built Runner still marks the issue done.
func (r *Runner) triggerLabel() string {
	if r.TriggerLabel != "" {
		return r.TriggerLabel
	}
	return defaultTriggerLabel
}

// blockedLabel returns the configured blocked label, falling back to the
// default when unset so a hand-built Runner still relabels correctly.
func (r *Runner) blockedLabel() string {
	if r.BlockedLabel != "" {
		return r.BlockedLabel
	}
	return defaultBlockedLabel
}

// Run executes the full pipeline for one issue and opens a PR on success. It
// returns the PR URL on success; on failure it returns an error describing
// the outcome (no-changes, red, etc.) and leaves the worktree in place for
// inspection.
func (r *Runner) Run(ctx context.Context, issueNum int) (string, error) {
	if len(r.Verify) == 0 {
		return "", fmt.Errorf("no verify command configured: run `romp init` or pass --verify")
	}

	owner, name, err := r.Git.Origin(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve origin: %w", err)
	}
	repo := owner + "/" + name
	r.logf("repo %s, issue #%d", repo, issueNum)

	if err := r.Git.Fetch(ctx); err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	base := r.Base
	if base == "" {
		base, err = r.Git.DefaultBranch(ctx)
		if err != nil {
			return "", fmt.Errorf("default branch: %w", err)
		}
	}

	issue, err := r.GH.Issue(ctx, repo, issueNum)
	if err != nil {
		return "", err
	}

	branch := fmt.Sprintf("romp-%d", issueNum)
	dir := filepath.Join(cacheDir(), owner+"-"+name, branch)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}

	if err := r.Git.AddWorktree(ctx, branch, dir, "origin/"+base); err != nil {
		return "", fmt.Errorf("worktree: %w", err)
	}

	promptText, err := r.Prompt.Render(prompt.Data{
		Issue:     fmt.Sprint(issueNum),
		Title:     issue.Title,
		Body:      issue.Body,
		Repo:      repo,
		Branch:    branch,
		Base:      base,
		URL:       issue.URL,
		Verify:    strings.Join(r.Verify, " && "),
		Protected: strings.Join(r.Protected, ", "),
		Ignore:    strings.Join(r.Ignore, ", "),
		Brief:     r.Brief,
	})
	if err != nil {
		return "", err
	}

	r.logf("worktree %s", dir)
	r.logf("running %s", r.Harness.Name())

	start := time.Now()
	runCtx := ctx
	var cancel context.CancelFunc
	if r.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	_, err = r.Harness.Run(runCtx, harness.Request{Dir: dir, Prompt: promptText, Model: r.Model, Effort: r.Effort, MaxTurns: r.MaxTurns})
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return "", err
	}
	r.logf("agent took %s", time.Since(start).Round(time.Second))

	gap, err := readBlocked(dir)
	if err != nil {
		return "", err
	}
	if gap != "" {
		if err := r.GH.Comment(ctx, repo, issueNum, blockedComment(gap)); err != nil {
			return "", fmt.Errorf("posting blocked comment: %w", err)
		}
		if err := r.GH.AddLabel(ctx, repo, issueNum, r.blockedLabel()); err != nil {
			return "", fmt.Errorf("relabelling %s: %w", r.blockedLabel(), err)
		}
		if err := r.Git.RemoveWorktree(ctx, dir); err != nil {
			r.logf("warning: cleanup worktree: %v", err)
		}
		if err := r.Git.DeleteBranch(ctx, branch); err != nil {
			r.logf("warning: delete branch: %v", err)
		}
		return "", fmt.Errorf("%w: %s", ErrBlocked, gap)
	}

	pr, err := readPR(dir, issue.Title, issueNum)
	if err != nil {
		return "", err
	}
	if err := removePRArtifact(dir); err != nil {
		return "", err
	}

	changed, err := r.Git.HasChanges(ctx, dir, "origin/"+base)
	if err != nil {
		return "", err
	}
	if !changed {
		return "", fmt.Errorf("%w: agent made no changes in %s", ErrNoChanges, dir)
	}

	if err := r.Git.CommitAll(ctx, dir, pr.Commit); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	r.logf("verify: %s", strings.Join(r.Verify, " && "))
	if err := r.verify(ctx, dir); err != nil {
		return "", fmt.Errorf("%w: %s failed: %v (worktree kept at %s)", ErrRed, strings.Join(r.Verify, " && "), err, dir)
	}

	if err := r.Git.Push(ctx, dir, branch); err != nil {
		return "", fmt.Errorf("push: %w", err)
	}

	url, err := r.GH.CreatePR(ctx, repo, pr.Title, prBody(pr.Body, issueNum), branch, base)
	if err != nil {
		return "", err
	}
	r.logf("PR: %s", url)

	// Label removal is the completion marker (ADR 0003): without it a later
	// watch re-claims this issue and opens a second PR for it.
	if err := r.GH.RemoveLabel(ctx, repo, issueNum, r.triggerLabel()); err != nil {
		return "", fmt.Errorf("removing %s label: %w", r.triggerLabel(), err)
	}

	if err := r.Git.RemoveWorktree(ctx, dir); err != nil {
		r.logf("warning: cleanup worktree: %v", err)
	}

	return url, nil
}

// verify re-runs each verification command itself in the worktree, in order. The
// agent's own claim that tests pass is not proof.
func (r *Runner) verify(ctx context.Context, dir string) error {
	for _, v := range r.Verify {
		fields := strings.Fields(v)
		if len(fields) == 0 {
			return fmt.Errorf("empty verify command")
		}
		cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s", out)
		}
		r.logf("verify ok (%s):\n%s", v, out)
	}
	return nil
}

func cacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(d, "romp")
}
