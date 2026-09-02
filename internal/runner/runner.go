// Package runner orchestrates a single romp job: fetch the issue, build a
// worktree, run the harness, independently verify, open a PR, and review it.
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
	"github.com/BRO3886/romp/internal/progress"
	"github.com/BRO3886/romp/internal/prompt"
	"github.com/BRO3886/romp/internal/review"
)

// defaultTriggerLabel is the label romp watch polls for, and the one a
// finished run removes as its completion marker.
const (
	defaultTriggerLabel          = "romp"
	defaultChangesRequestedLabel = "romp:changes-requested"
)

// ErrTimeout is returned when the job timeout expires while the harness is
// running. The agent is killed and the job stays labelled so it can retry.
var ErrTimeout = errors.New("timeout")

// ErrNoChanges is returned when the agent finished without touching the repo.
var ErrNoChanges = errors.New("no-changes")

// ErrRed is returned when a verification command fails after the agent finished.
var ErrRed = errors.New("red")

// ErrChangesRequested is returned when blocking review findings remain after
// the single fix round.
var ErrChangesRequested = errors.New("changes-requested")

// GitOps is the git surface a run needs: read the remote and the default
// branch, manage the job worktree and branch, and publish the result.
type GitOps interface {
	Origin(ctx context.Context) (owner, name string, err error)
	DefaultBranch(ctx context.Context) (string, error)
	RefreshBranch(ctx context.Context, branch string) (commit string, err error)
	AddWorktree(ctx context.Context, branch, dir, base string) error
	RemoveWorktree(ctx context.Context, dir string) error
	DeleteBranch(ctx context.Context, branch string) error
	HasChanges(ctx context.Context, dir, base string) (bool, error)
	ChangedFiles(ctx context.Context, dir, base string) ([]string, error)
	Diff(ctx context.Context, dir, base string) (string, error)
	BranchLog(ctx context.Context, dir, base string) (string, error)
	CommitAll(ctx context.Context, dir, msg string) error
	Push(ctx context.Context, dir, branch string) error
}

// GHOps is the GitHub surface a run needs: read the issue, report a block,
// move labels, open the PR, and record review passes on it.
type GHOps interface {
	Issue(ctx context.Context, repo string, number int) (gh.Issue, error)
	Comment(ctx context.Context, repo string, number int, body string) error
	CommentPR(ctx context.Context, repo, pullRequest, body string) error
	AddLabel(ctx context.Context, repo string, number int, label string) error
	RemoveLabel(ctx context.Context, repo string, number int, label string) error
	CreatePR(ctx context.Context, repo, title, body, head, base string) (string, error)
}

// SessionStore records a successful harness run on its in-flight job.
type SessionStore interface {
	SetSessionID(ctx context.Context, repo string, issue int, sessionID string) error
}

// ReviewInstrumentationStore records review calibration facts on an in-flight job.
type ReviewInstrumentationStore interface {
	SetReviewInstrumentation(ctx context.Context, repo string, issue int, metrics review.Instrumentation) error
}

// Runner wires the ports together for a single one-shot job.
type Runner struct {
	Harness               harness.Harness
	ReviewHarness         harness.Harness
	Sessions              SessionStore
	ReviewInstrumentation ReviewInstrumentationStore
	Git                   GitOps
	GH                    GHOps
	Prompt                *prompt.Renderer
	Verify                []string
	Model                 string
	ReviewModel           string
	ReviewEnabled         bool
	Effort                string
	MaxTurns              int
	Codename              string
	Base                  string
	Timeout               time.Duration
	Protected             []string
	Ignore                []string
	Brief                 string
	TriggerLabel          string
	BlockedLabel          string
	ChangesRequestedLabel string
	Stderr                io.Writer
	Progress              progress.Sink
}

func (r *Runner) logf(format string, a ...any) {
	prefix := ""
	if r.Codename != "" {
		prefix = "[" + r.Codename + "] "
	}
	fmt.Fprintf(r.Stderr, "%s  %s%s\n", time.Now().Format("15:04:05"), prefix, fmt.Sprintf(format, a...))
}

func (r *Runner) progress(issue int, phase progress.Phase, detail, harnessName string) {
	r.Progress.Emit(progress.Event{Issue: issue, Phase: phase, Detail: detail, Harness: harnessName})
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

func (r *Runner) changesRequestedLabel() string {
	if r.ChangesRequestedLabel != "" {
		return r.ChangesRequestedLabel
	}
	return defaultChangesRequestedLabel
}

// Run executes the full pipeline for one issue and opens a PR on success. It
// returns the PR URL once one exists, including with a later review error. On
// earlier failure it returns an error describing the outcome (no-changes, red,
// etc.) and leaves the worktree in place for inspection.
func (r *Runner) Run(ctx context.Context, issueNum int) (string, error) {
	if len(r.Verify) == 0 {
		return "", fmt.Errorf("no verify command configured: run `romp init` or pass --verify")
	}

	owner, name, err := r.Git.Origin(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve origin: %w", err)
	}
	repo := owner + "/" + name
	metrics := review.Instrumentation{}
	r.logf("repo %s, issue #%d", repo, issueNum)
	r.progress(issueNum, progress.PhasePreparing, "refreshing the base branch", "")

	base := r.Base
	if base == "" {
		base, err = r.Git.DefaultBranch(ctx)
		if err != nil {
			return "", fmt.Errorf("default branch: %w", err)
		}
	}
	baseCommit, err := r.Git.RefreshBranch(ctx, base)
	if err != nil {
		return "", fmt.Errorf("refresh base: %w", err)
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

	if err := r.Git.AddWorktree(ctx, branch, dir, baseCommit); err != nil {
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
	r.progress(issueNum, progress.PhaseAgent, "agent working", r.Harness.Name())

	start := time.Now()
	runCtx := ctx
	var cancel context.CancelFunc
	if r.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	_, err = r.runHarness(runCtx, repo, issueNum, r.Harness, harness.Request{Dir: dir, Prompt: promptText, Model: r.Model, Effort: r.Effort, MaxTurns: r.MaxTurns})
	metrics.BuilderDurationMS += time.Since(start).Milliseconds()
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

	changed, err := r.Git.HasChanges(ctx, dir, baseCommit)
	if err != nil {
		return "", err
	}
	if !changed {
		if err := r.Git.RemoveWorktree(runCtx, dir); err != nil {
			r.logf("warning: cleanup worktree: %v", err)
		}
		if err := r.Git.DeleteBranch(runCtx, branch); err != nil {
			r.logf("warning: delete branch: %v", err)
		}
		return "", fmt.Errorf("%w: agent made no changes in %s", ErrNoChanges, dir)
	}

	if err := r.Git.CommitAll(runCtx, dir, pr.Commit); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	r.logf("verify: %s", strings.Join(r.Verify, " && "))
	verification, err := r.verifyWithResults(runCtx, dir, issueNum, progress.PhaseVerifying)
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return "", fmt.Errorf("%w: %s failed: %v (worktree kept at %s)", ErrRed, strings.Join(r.Verify, " && "), err, dir)
	}

	if err := r.Git.Push(runCtx, dir, branch); err != nil {
		return "", fmt.Errorf("push: %w", err)
	}
	r.progress(issueNum, progress.PhasePublishing, "opening pull request", "")

	url, err := r.GH.CreatePR(runCtx, repo, pr.Title, prBody(pr.Body, issueNum), branch, base)
	if err != nil {
		return "", err
	}
	r.logf("PR: %s", url)

	if r.ReviewEnabled {
		outcome, plan, pass, err := r.review(runCtx, repo, issueNum, issue, dir, baseCommit, verification)
		if err != nil {
			if plan.HasCode {
				metrics.ReviewRan = true
				metrics.Passes = append(metrics.Passes, pass)
				r.logReviewPass(len(metrics.Passes), pass, err)
			}
			recordErr := r.recordReviewInstrumentation(runCtx, repo, issueNum, metrics)
			commentErr := r.GH.CommentPR(ctx, repo, url, reviewFailureComment(1))
			if commentErr != nil {
				commentErr = fmt.Errorf("posting review failure for pass 1: %w", commentErr)
			}
			return url, errors.Join(err, recordErr, commentErr)
		}
		if plan.HasCode {
			metrics.ReviewRan = true
			metrics.Passes = append(metrics.Passes, pass)
			r.logReviewPass(len(metrics.Passes), pass, err)
			if err := r.GH.CommentPR(ctx, repo, url, reviewPassComment(1, outcome)); err != nil {
				return url, fmt.Errorf("posting review pass 1: %w", err)
			}
		} else {
			metrics.SkipReason = review.SkipDocsOnly
			r.logf("review skipped: %s", metrics.SkipReason)
		}
		if recordErr := r.recordReviewInstrumentation(runCtx, repo, issueNum, metrics); recordErr != nil {
			return url, recordErr
		}
		if plan.HasCode && outcome.Verdict == review.VerdictFix {
			metrics.FixRoundFired = true
			r.logf("review fix: running %s", r.Harness.Name())
			r.progress(issueNum, progress.PhaseFixing, "agent addressing review findings", r.Harness.Name())
			fixPrompt := promptText + "\n\nADDITIONAL CONSTRAINTS FROM THE REVIEW GATE:\n" + formatBlockingFindings(outcome.Findings)
			fixStarted := time.Now()
			if _, err := r.runHarness(runCtx, repo, issueNum, r.Harness, harness.Request{Dir: dir, Prompt: fixPrompt, Model: r.Model, Effort: r.Effort, MaxTurns: r.MaxTurns}); err != nil {
				metrics.BuilderDurationMS += time.Since(fixStarted).Milliseconds()
				metrics.FixRoundOutcome = "error"
				recordErr := r.recordReviewInstrumentation(runCtx, repo, issueNum, metrics)
				if runCtx.Err() == context.DeadlineExceeded {
					return url, errors.Join(fmt.Errorf("%w: %v", ErrTimeout, err), recordErr)
				}
				return url, errors.Join(err, recordErr)
			}
			metrics.BuilderDurationMS += time.Since(fixStarted).Milliseconds()
			if _, err := readPR(dir, issue.Title, issueNum); err != nil {
				return url, err
			}
			if err := removePRArtifact(dir); err != nil {
				return url, err
			}
			fixCommit := fmt.Sprintf("fix: address review findings for #%d", issueNum)
			if err := r.Git.CommitAll(runCtx, dir, fixCommit); err != nil {
				return url, fmt.Errorf("commit fix round: %w", err)
			}
			verification, err = r.verifyWithResults(runCtx, dir, issueNum, progress.PhaseReverifying)
			if err != nil {
				metrics.FixRoundOutcome = "red"
				recordErr := r.recordReviewInstrumentation(runCtx, repo, issueNum, metrics)
				if runCtx.Err() == context.DeadlineExceeded {
					return url, errors.Join(fmt.Errorf("%w: %v", ErrTimeout, err), recordErr)
				}
				return url, errors.Join(fmt.Errorf("%w: %s failed after fix round: %v (worktree kept at %s)", ErrRed, strings.Join(r.Verify, " && "), err, dir), recordErr)
			}
			if err := r.Git.Push(runCtx, dir, branch); err != nil {
				metrics.FixRoundOutcome = "error"
				return url, errors.Join(fmt.Errorf("push fix round: %w", err), r.recordReviewInstrumentation(runCtx, repo, issueNum, metrics))
			}
			outcome, _, pass, err := r.reviewAfterFix(runCtx, repo, issueNum, issue, dir, baseCommit, verification)
			metrics.Passes = append(metrics.Passes, pass)
			r.logReviewPass(len(metrics.Passes), pass, err)
			if err != nil {
				metrics.FixRoundOutcome = "error"
				recordErr := r.recordReviewInstrumentation(runCtx, repo, issueNum, metrics)
				commentErr := r.GH.CommentPR(ctx, repo, url, reviewFailureComment(2))
				if commentErr != nil {
					commentErr = fmt.Errorf("posting review failure for pass 2: %w", commentErr)
				}
				return url, errors.Join(err, recordErr, commentErr)
			}
			if err := r.GH.CommentPR(ctx, repo, url, reviewPassComment(2, outcome)); err != nil {
				metrics.FixRoundOutcome = "error"
				return url, errors.Join(fmt.Errorf("posting review pass 2: %w", err), r.recordReviewInstrumentation(runCtx, repo, issueNum, metrics))
			}
			if outcome.Verdict == review.VerdictFix {
				metrics.FixRoundOutcome = review.FixBlocking
				if err := r.recordReviewInstrumentation(runCtx, repo, issueNum, metrics); err != nil {
					return url, err
				}
				blocking := formatBlockingFindings(outcome.Findings)
				if err := r.GH.AddLabel(runCtx, repo, issueNum, r.changesRequestedLabel()); err != nil {
					return url, fmt.Errorf("adding %s label: %w", r.changesRequestedLabel(), err)
				}
				return url, fmt.Errorf("%w: blocking review findings remain (worktree kept at %s): %s", ErrChangesRequested, dir, blocking)
			}
			metrics.FixRoundOutcome = review.FixApproved
			if err := r.recordReviewInstrumentation(runCtx, repo, issueNum, metrics); err != nil {
				return url, err
			}
		}
	} else {
		metrics.SkipReason = review.SkipDisabled
		r.logf("review skipped: %s", metrics.SkipReason)
		if err := r.recordReviewInstrumentation(runCtx, repo, issueNum, metrics); err != nil {
			return url, err
		}
	}

	// Label removal is the completion marker (ADR 0003): without it a later
	// watch re-claims this issue and opens a second PR for it.
	if err := r.GH.RemoveLabel(ctx, repo, issueNum, r.triggerLabel()); err != nil {
		return url, fmt.Errorf("removing %s label: %w", r.triggerLabel(), err)
	}

	if err := r.Git.RemoveWorktree(ctx, dir); err != nil {
		r.logf("warning: cleanup worktree: %v", err)
	}

	return url, nil
}

func (r *Runner) recordReviewInstrumentation(ctx context.Context, repo string, issue int, metrics review.Instrumentation) error {
	if r.ReviewInstrumentation == nil {
		return nil
	}
	if err := r.ReviewInstrumentation.SetReviewInstrumentation(context.WithoutCancel(ctx), repo, issue, metrics); err != nil {
		return fmt.Errorf("recording review instrumentation: %w", err)
	}
	return nil
}

func (r *Runner) logReviewPass(number int, pass review.PassInstrumentation, err error) {
	if err != nil {
		r.logf("review pass %d: error, blocking %d, non-blocking %d, nits %d, took %s", number, pass.Blocking, pass.NonBlocking, pass.Nit, time.Duration(pass.DurationMS)*time.Millisecond)
		return
	}
	r.logf("review pass %d: %s, blocking %d, non-blocking %d, nits %d, took %s", number, pass.Verdict, pass.Blocking, pass.NonBlocking, pass.Nit, time.Duration(pass.DurationMS)*time.Millisecond)
}

// verify re-runs each verification command itself in the worktree, in order. The
// agent's own claim that tests pass is not proof.
func (r *Runner) verify(ctx context.Context, dir string) error {
	_, err := r.verifyWithResults(ctx, dir, 0, progress.PhaseVerifying)
	return err
}

func (r *Runner) verifyWithResults(ctx context.Context, dir string, issue int, phase progress.Phase) ([]prompt.VerificationResult, error) {
	results := make([]prompt.VerificationResult, 0, len(r.Verify))
	for i, v := range r.Verify {
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("empty verify command")
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", v)
		cmd.Dir = dir
		r.progress(issue, phase, fmt.Sprintf("%d/%d %s", i+1, len(r.Verify), v), "")
		started := time.Now()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("%s", out)
		}
		results = append(results, prompt.VerificationResult{Command: v, ExitCode: 0, Output: string(out)})
		r.logf("verify ok (%s) in %s", v, time.Since(started).Round(time.Millisecond))
	}
	return results, nil
}

func (r *Runner) runHarness(ctx context.Context, repo string, issue int, h harness.Harness, req harness.Request) (harness.Result, error) {
	result, err := h.Run(ctx, req)
	if err != nil {
		return result, err
	}
	if result.SessionID != "" {
		r.logf("session %s", result.SessionID)
		if r.Sessions != nil {
			if err := r.Sessions.SetSessionID(context.WithoutCancel(ctx), repo, issue, result.SessionID); err != nil {
				return result, fmt.Errorf("recording session: %w", err)
			}
		}
	}
	return result, nil
}

func cacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(d, "romp")
}
