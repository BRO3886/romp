// Package git wraps the git CLI for the operations romp needs: worktrees,
// branches, commits, pushes, and reading the origin remote.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// Repo is the main checkout romp was started in.
type Repo struct {
	Root   string
	syncMu sync.Mutex
}

func (r *Repo) run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	} else {
		cmd.Dir = r.Root
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// FindRoot returns the top-level directory of the git repository containing
// dir (the current directory when dir is empty).
func FindRoot(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// Version returns the installed git version, e.g. "2.50.1".
func Version(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git --version: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// Origin returns the owner and repo name parsed from the origin remote URL.
// It understands scp-like (git@github.com:owner/name.git) and https forms.
func (r *Repo) Origin(ctx context.Context) (owner, name string, err error) {
	url, err := r.run(ctx, "", "remote", "get-url", "origin")
	if err != nil {
		return "", "", err
	}
	return parseRemote(url)
}

func parseRemote(url string) (owner, name string, err error) {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSuffix(url, "/")

	var path string
	switch {
	case strings.Contains(url, "://"):
		// https://github.com/owner/name
		rest := url[strings.Index(url, "://")+3:]
		if i := strings.Index(rest, "/"); i >= 0 {
			path = rest[i+1:]
		} else {
			return "", "", fmt.Errorf("unrecognized remote url %q", url)
		}
	case strings.Contains(url, ":"):
		// git@github.com:owner/name
		path = url[strings.LastIndex(url, ":")+1:]
	default:
		return "", "", fmt.Errorf("unrecognized remote url %q", url)
	}

	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("unrecognized remote url %q", url)
	}
	return parts[0], parts[1], nil
}

// DefaultBranch returns the current default branch advertised by origin.
func (r *Repo) DefaultBranch(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "", "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolving remote default branch: %w", err)
	}
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "ref:" || fields[2] != "HEAD" {
			continue
		}
		const heads = "refs/heads/"
		if strings.HasPrefix(fields[1], heads) {
			return strings.TrimPrefix(fields[1], heads), nil
		}
	}
	return "", fmt.Errorf("remote HEAD does not identify a default branch; configure base explicitly")
}

// RefreshBranch fetches one origin branch and returns its exact fetched commit.
func (r *Repo) RefreshBranch(ctx context.Context, branch string) (string, error) {
	r.syncMu.Lock()
	defer r.syncMu.Unlock()

	if _, err := r.run(ctx, "", "check-ref-format", "--branch", branch); err != nil {
		return "", fmt.Errorf("invalid base branch %q: %w", branch, err)
	}
	remoteRef := "refs/heads/" + branch
	trackingRef := "refs/remotes/origin/" + branch
	refspec := "+" + remoteRef + ":" + trackingRef
	if _, err := r.run(ctx, "", "fetch", "--no-tags", "origin", refspec); err != nil {
		return "", fmt.Errorf("refreshing origin/%s: %w", branch, err)
	}
	commit, err := r.run(ctx, "", "rev-parse", "--verify", trackingRef+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolving refreshed origin/%s: %w", branch, err)
	}
	return commit, nil
}

// AddWorktree creates a fresh branch off base in a new worktree at dir. It
// best-effort cleans up a prior worktree/branch of the same name so runs are
// repeatable; romp owns the "romp-*" namespace.
func (r *Repo) AddWorktree(ctx context.Context, branch, dir, base string) error {
	_, _ = r.run(ctx, "", "worktree", "remove", "--force", dir)
	_, _ = r.run(ctx, "", "branch", "-D", branch)
	_, err := r.run(ctx, "", "worktree", "add", "-b", branch, dir, base)
	return err
}

// RemoveWorktree deletes the worktree at dir.
func (r *Repo) RemoveWorktree(ctx context.Context, dir string) error {
	_, err := r.run(ctx, "", "worktree", "remove", "--force", dir)
	return err
}

// DeleteBranch force-deletes a local branch.
func (r *Repo) DeleteBranch(ctx context.Context, branch string) error {
	_, err := r.run(ctx, "", "branch", "-D", branch)
	return err
}

// HasChanges reports whether the worktree at dir differs from base, either as
// uncommitted changes or commits on top of base (the agent may commit itself).
func (r *Repo) HasChanges(ctx context.Context, dir, base string) (bool, error) {
	out, err := r.run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(out) != "" {
		return true, nil
	}
	out, err = r.run(ctx, dir, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(out) != "0", nil
}

// CommitAll stages everything in dir and commits it. If the agent already
// committed (HEAD moved), the resulting no-op commit is ignored.
func (r *Repo) CommitAll(ctx context.Context, dir, msg string) error {
	if _, err := r.run(ctx, dir, "add", "-A"); err != nil {
		return err
	}
	if _, err := r.run(ctx, dir, "commit", "-m", msg); err != nil {
		if strings.Contains(err.Error(), "nothing to commit") {
			return nil
		}
		return err
	}
	return nil
}

// Push pushes branch from the worktree to origin.
func (r *Repo) Push(ctx context.Context, dir, branch string) error {
	_, err := r.run(ctx, dir, "push", "-u", "origin", branch)
	return err
}

// ChangedFiles lists paths changed from base in Git's stable name-only order.
func (r *Repo) ChangedFiles(ctx context.Context, dir, base string) ([]string, error) {
	out, err := r.run(ctx, dir, "diff", "--name-only", base+"...HEAD")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return []string{}, nil
	}
	return strings.Split(out, "\n"), nil
}

// Diff returns the complete patch from base through the worktree HEAD.
func (r *Repo) Diff(ctx context.Context, dir, base string) (string, error) {
	return r.run(ctx, dir, "diff", "--no-ext-diff", base+"...HEAD")
}

// BranchLog returns the commits introduced after base.
func (r *Repo) BranchLog(ctx context.Context, dir, base string) (string, error) {
	return r.run(ctx, dir, "log", "--format=%h %s", base+"..HEAD")
}
