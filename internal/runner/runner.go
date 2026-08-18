// Package runner orchestrates a single romp job: fetch the issue, build a
// worktree, run the harness, independently verify, and open a PR.
package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/git"
	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/prompt"
)

// Runner wires the ports together for a single one-shot job.
type Runner struct {
	Harness harness.Harness
	Git     *git.Repo
	GH      *gh.Client
	Prompt  *prompt.Renderer
	Verify  string
	Stderr  io.Writer
}

func (r *Runner) logf(format string, a ...any) {
	fmt.Fprintf(r.Stderr, format+"\n", a...)
}

// Run executes the full pipeline for one issue and opens a PR on success.
// On failure it returns an error describing the outcome (no-changes, red,
// etc.) and leaves the worktree in place for inspection.
func (r *Runner) Run(ctx context.Context, issueNum int) error {
	owner, name, err := r.Git.Origin(ctx)
	if err != nil {
		return fmt.Errorf("resolve origin: %w", err)
	}
	repo := owner + "/" + name
	r.logf("repo %s, issue #%d", repo, issueNum)

	if err := r.Git.Fetch(ctx); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	base, err := r.Git.DefaultBranch(ctx)
	if err != nil {
		return fmt.Errorf("default branch: %w", err)
	}

	issue, err := r.GH.Issue(ctx, repo, issueNum)
	if err != nil {
		return err
	}

	branch := fmt.Sprintf("romp-%d", issueNum)
	dir := filepath.Join(cacheDir(), owner+"-"+name, branch)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}

	if err := r.Git.AddWorktree(ctx, branch, dir, "origin/"+base); err != nil {
		return fmt.Errorf("worktree: %w", err)
	}

	promptText, err := r.Prompt.Render(prompt.Data{
		Issue:  fmt.Sprint(issueNum),
		Title:  issue.Title,
		Body:   issue.Body,
		Repo:   repo,
		Branch: branch,
		Base:   base,
		URL:    issue.URL,
		Verify: r.Verify,
	})
	if err != nil {
		return err
	}

	r.logf("worktree %s", dir)
	r.logf("running %s", r.Harness.Name())

	start := time.Now()
	res, err := r.Harness.Run(ctx, harness.Request{Dir: dir, Prompt: promptText})
	if err != nil {
		return err
	}
	r.logf("agent took %s", time.Since(start).Round(time.Second))
	if res.Output != "" {
		r.logf("%s", res.Output)
	}

	gap, err := readBlocked(dir)
	if err != nil {
		return err
	}
	if gap != "" {
		if err := r.GH.Comment(ctx, repo, issueNum, blockedComment(gap)); err != nil {
			return fmt.Errorf("posting blocked comment: %w", err)
		}
		if err := r.GH.AddLabel(ctx, repo, issueNum, blockedLabel); err != nil {
			return fmt.Errorf("relabelling %s: %w", blockedLabel, err)
		}
		if err := r.Git.RemoveWorktree(ctx, dir); err != nil {
			r.logf("warning: cleanup worktree: %v", err)
		}
		if err := r.Git.DeleteBranch(ctx, branch); err != nil {
			r.logf("warning: delete branch: %v", err)
		}
		return fmt.Errorf("%w: %s", ErrBlocked, gap)
	}

	pr, err := readPR(dir, issue.Title, issueNum)
	if err != nil {
		return err
	}
	if err := removePRArtifact(dir); err != nil {
		return err
	}

	changed, err := r.Git.HasChanges(ctx, dir, "origin/"+base)
	if err != nil {
		return err
	}
	if !changed {
		return fmt.Errorf("no-changes: agent made no changes in %s", dir)
	}

	if err := r.Git.CommitAll(ctx, dir, pr.Commit); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	r.logf("verify: %s", r.Verify)
	if err := r.verify(ctx, dir); err != nil {
		return fmt.Errorf("red: %s failed: %v (worktree kept at %s)", r.Verify, err, dir)
	}

	if err := r.Git.Push(ctx, dir, branch); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	url, err := r.GH.CreatePR(ctx, repo, pr.Title, withCloses(pr.Body, issueNum), branch, base)
	if err != nil {
		return err
	}

	if err := r.Git.RemoveWorktree(ctx, dir); err != nil {
		r.logf("warning: cleanup worktree: %v", err)
	}

	r.logf("PR: %s", url)
	return nil
}

// verify re-runs the test command itself in the worktree. The agent's own
// claim that tests pass is not proof.
func (r *Runner) verify(ctx context.Context, dir string) error {
	fields := strings.Fields(r.Verify)
	if len(fields) == 0 {
		return fmt.Errorf("empty verify command")
	}
	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", out)
	}
	r.logf("verify ok:\n%s", out)
	return nil
}

func cacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(d, "romp")
}
