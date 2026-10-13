// Package watch polls a repo for trigger-labelled issues and runs each in a
// worktree at a bounded width until a signal stops it.
package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/progress"
	"github.com/BRO3886/romp/internal/runner"
)

// GHOps is the GitHub surface watch needs: list labelled issues and move
// labels and comments on them.
type GHOps interface {
	ListIssues(ctx context.Context, repo, label string) ([]gh.Issue, error)
	AddLabel(ctx context.Context, repo string, number int, label string) error
	RemoveLabel(ctx context.Context, repo string, number int, label string) error
	Assign(ctx context.Context, repo string, number int) error
	Unassign(ctx context.Context, repo string, number int) error
	Comment(ctx context.Context, repo string, number int, body string) error
	OpenPR(ctx context.Context, repo, branch string) (int, error)
}

// Store is the job-table surface watch needs.
type Store interface {
	Claim(ctx context.Context, repo string, issue int, branch string) (bool, error)
	Delete(ctx context.Context, repo string, issue int) error
	Finish(ctx context.Context, o job.Outcome) error
}

// Watcher claims trigger-labelled issues and runs them at Width concurrency.
type Watcher struct {
	Repo     string
	Trigger  string
	Claim    string
	Blocked  string
	Width    int
	Interval time.Duration

	GH         GHOps
	Store      Store
	RunJob     func(ctx context.Context, issue int) (string, error)
	AllowClaim func() bool
	// CleanJob removes a cancelled job's worktree and branch. It is the
	// runner's counterpart, invoked only for an abandon so the watcher does
	// not need git access.
	CleanJob func(ctx context.Context, issue int) error
	Logf     func(format string, a ...any)
	Stderr   io.Writer
	Progress progress.Sink

	mu           sync.Mutex
	cancels      map[int]context.CancelFunc
	cancelled    map[int]bool
	terminal     map[int]bool
	slots        chan struct{}
	workers      sync.WaitGroup
	admissions   uint64
	lifecycleErr error
}

// PollOne admits at most one job. Its caller owns polling and shutdown.
func (w *Watcher) PollOne(ctx context.Context) (bool, error) {
	if w.slots == nil {
		w.slots = make(chan struct{}, max(1, w.Width))
		for range cap(w.slots) {
			w.slots <- struct{}{}
		}
	}
	if len(w.slots) == 0 {
		return false, nil
	}
	before := w.admissions
	err := w.claimBatchLimit(ctx, w.slots, &w.workers, context.WithoutCancel(ctx), 1)
	return w.admissions != before, err
}

// Cancel requests abandonment of one active job.
func (w *Watcher) Cancel(issue int) bool { return w.cancelIssue(issue) }

// CancelAll abandons every owned job before shutdown.
func (w *Watcher) CancelAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for issue, cancel := range w.cancels {
		if w.terminal[issue] {
			continue
		}
		w.cancelled[issue] = true
		cancel()
	}
}

// Wait waits for outcomes and cleanup. No new admissions may race this call.
func (w *Watcher) Wait() error {
	w.workers.Wait()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lifecycleErr
}

func (w *Watcher) recordLifecycleError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lifecycleErr = errors.Join(w.lifecycleErr, err)
}

// ActiveIssues includes jobs still releasing labels or cleaning worktrees.
func (w *Watcher) ActiveIssues() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	issues := make([]int, 0, len(w.cancels))
	for issue := range w.cancels {
		issues = append(issues, issue)
	}
	return issues
}

// Active returns the number of jobs still completing their lifecycle.
func (w *Watcher) Active() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.cancels)
}

func (w *Watcher) logf(format string, a ...any) {
	msg := fmt.Sprintf("%s  %s", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
	if w.Logf != nil {
		w.Logf("%s", msg)
		return
	}
	stderr := w.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	fmt.Fprintln(stderr, msg)
}

// claimBatch lists trigger-labelled issues and dispatches each unclaimed one,
// stopping early when every slot is busy so the leftover issues stay unclaimed
// for another watcher (or the next tick).
func (w *Watcher) claimBatch(ctx context.Context, slots chan struct{}, wg *sync.WaitGroup, jobCtx context.Context) error {
	return w.claimBatchLimit(ctx, slots, wg, jobCtx, 0)
}

func (w *Watcher) claimBatchLimit(ctx context.Context, slots chan struct{}, wg *sync.WaitGroup, jobCtx context.Context, limit int) error {
	issues, err := w.GH.ListIssues(ctx, w.Repo, w.Trigger)
	if err != nil {
		return err
	}
	for _, iss := range issues {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if iss.HasLabel(w.Claim) || iss.HasLabel(w.Blocked) {
			continue
		}
		// Reconcile: an open PR for this issue's branch means the work is
		// already done, so drop the trigger label instead of running a second
		// agent and opening a duplicate PR. A reconcile check failure defers
		// the issue to the next tick rather than risking a duplicate run.
		if pr, err := w.GH.OpenPR(ctx, w.Repo, branchFor(iss.Number)); err != nil {
			w.logf("#%d: reconcile: %v", iss.Number, err)
			continue
		} else if pr != 0 {
			w.logf("#%d: PR #%d already open; dropping trigger label", iss.Number, pr)
			if err := w.GH.RemoveLabel(ctx, w.Repo, iss.Number, w.Trigger); err != nil {
				w.logf("#%d: removing trigger label: %v", iss.Number, err)
			}
			if err := w.GH.Comment(ctx, w.Repo, iss.Number,
				fmt.Sprintf("PR #%d is already open; removed the %q label so this issue is not worked twice.", pr, w.Trigger)); err != nil {
				w.logf("#%d: reconcile comment: %v", iss.Number, err)
			}
			continue
		}
		select {
		case <-slots:
			if w.AllowClaim != nil && !w.AllowClaim() {
				slots <- struct{}{}
				return nil
			}
			if !w.claim(ctx, iss) {
				slots <- struct{}{}
				continue
			}
			wg.Add(1)
			runCtx, cancel := context.WithCancel(jobCtx)
			w.register(iss.Number, cancel)
			w.admissions++
			go w.runRegistered(runCtx, cancel, iss, slots, wg)
			if limit == 1 {
				return nil
			}
		default:
			return nil
		}
	}
	return nil
}

func (w *Watcher) claim(ctx context.Context, iss gh.Issue) bool {
	ok, err := w.Store.Claim(ctx, w.Repo, iss.Number, branchFor(iss.Number))
	if err != nil {
		w.logf("#%d: claim: %v", iss.Number, err)
		return false
	}
	if !ok {
		return false
	}
	if err := w.GH.AddLabel(ctx, w.Repo, iss.Number, w.Claim); err != nil {
		w.logf("#%d: adding claim label: %v", iss.Number, err)
		_ = w.Store.Delete(ctx, w.Repo, iss.Number)
		return false
	}
	if err := w.GH.Assign(ctx, w.Repo, iss.Number); err != nil {
		w.logf("#%d: assigning: %v", iss.Number, err)
		_ = w.GH.RemoveLabel(ctx, w.Repo, iss.Number, w.Claim)
		_ = w.Store.Delete(ctx, w.Repo, iss.Number)
		return false
	}
	w.logf("claimed #%d", iss.Number)
	w.Progress.Emit(progress.Event{Issue: iss.Number, Phase: progress.PhaseClaiming, Detail: iss.Title})
	return true
}

// branchFor is the job branch name for an issue.
func branchFor(issue int) string { return fmt.Sprintf("romp-%d", issue) }

// register records the cancel function for a running job.
func (w *Watcher) register(issue int, cancel context.CancelFunc) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancels == nil {
		w.cancels = map[int]context.CancelFunc{}
		w.cancelled = map[int]bool{}
		w.terminal = map[int]bool{}
	}
	w.cancels[issue] = cancel
}

func (w *Watcher) unregister(issue int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.cancels, issue)
	delete(w.cancelled, issue)
	delete(w.terminal, issue)
}

// cancelIssue kills the job for issue and records it as cancelled, reporting
// whether such a job was running.
func (w *Watcher) cancelIssue(issue int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	cancel, ok := w.cancels[issue]
	if !ok || w.terminal[issue] {
		return false
	}
	w.cancelled[issue] = true
	cancel()
	return true
}

func (w *Watcher) wasCancelled(issue int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cancelled[issue]
}

func (w *Watcher) finishExecution(issue int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.terminal[issue] = true
	return w.cancelled[issue]
}

func (w *Watcher) runJob(ctx context.Context, iss gh.Issue, slots chan<- struct{}, wg *sync.WaitGroup) {
	runCtx, cancel := context.WithCancel(ctx)
	w.register(iss.Number, cancel)
	w.runRegistered(runCtx, cancel, iss, slots, wg)
}

func (w *Watcher) runRegistered(ctx context.Context, cancel context.CancelFunc, iss gh.Issue, slots chan<- struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() { slots <- struct{}{} }()
	defer cancel()
	defer w.unregister(iss.Number)
	unassign := true
	defer func() { w.release(ctx, iss, unassign) }()
	prURL, err := w.RunJob(ctx, iss.Number)
	cancelled := w.finishExecution(iss.Number)
	unassign = cancelled || err != nil
	outcome := classifyOutcome(err)
	if cancelled {
		outcome = "cancelled"
	}
	// The record survives cancellation like release does, so a force-killed
	// job still lands in history.
	if err := w.Store.Finish(context.WithoutCancel(ctx), job.Outcome{
		Repo:       w.Repo,
		Issue:      iss.Number,
		Outcome:    outcome,
		Branch:     branchFor(iss.Number),
		PRURL:      prURL,
		Detail:     detailOf(err),
		FinishedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		w.logf("#%d: recording history: %v", iss.Number, err)
		w.recordLifecycleError(fmt.Errorf("recording %s#%d outcome: %w", w.Repo, iss.Number, err))
	}
	phase := progress.PhaseDone
	if outcome != "done" {
		phase = progress.PhaseFailed
	}
	w.Progress.Emit(progress.Event{Issue: iss.Number, Phase: phase, Detail: detailOf(err), Outcome: outcome, URL: prURL, Terminal: true})

	switch {
	case cancelled:
		w.logf("#%d: cancelled", iss.Number)
	case err == nil:
		w.logf("#%d: done", iss.Number)
	case errors.Is(err, runner.ErrBlocked):
		w.logf("#%d: blocked", iss.Number)
	case errors.Is(err, runner.ErrTimeout):
		w.logf("#%d: timeout", iss.Number)
	case errors.Is(err, runner.ErrChangesRequested):
		w.logf("#%d: changes-requested", iss.Number)
	default:
		w.logf("#%d: %v", iss.Number, err)
	}

	// Failed jobs are dropped from the pending queue so the next poll does not
	// immediately repeat the same failure. Blocked jobs keep the trigger label
	// because the blocked label excludes them while leaving the issue visible.
	if cancelled || (err != nil && !errors.Is(err, runner.ErrBlocked)) {
		if err := w.GH.RemoveLabel(context.WithoutCancel(ctx), w.Repo, iss.Number, w.Trigger); err != nil {
			w.logf("#%d: removing trigger label: %v", iss.Number, err)
			if cancelled {
				w.recordLifecycleError(err)
			}
		}
	}

	// Abandon (ADR 0009): a cancelled job is dropped, not retried, so its
	// worktree and branch are cleaned up.
	if cancelled {
		if w.CleanJob != nil {
			if err := w.CleanJob(context.WithoutCancel(ctx), iss.Number); err != nil {
				w.logf("#%d: cleaning up job: %v", iss.Number, err)
				w.recordLifecycleError(err)
			}
		}
	}
}

// classifyOutcome maps a run error to the outcome taxonomy in the README.
// Failures outside it (git or gh infrastructure errors) are recorded as
// "error" rather than guessed at.
func classifyOutcome(err error) string {
	switch {
	case err == nil:
		return "done"
	case errors.Is(err, runner.ErrBlocked):
		return "blocked"
	case errors.Is(err, runner.ErrTimeout):
		return "timeout"
	case errors.Is(err, runner.ErrNoChanges):
		return "no-changes"
	case errors.Is(err, runner.ErrRed):
		return "red"
	case errors.Is(err, runner.ErrChangesRequested):
		return "changes-requested"
	default:
		return "error"
	}
}

func detailOf(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// release drops the claim label and in-flight row on every terminal state.
// It removes the assignee unless the job opened a pull request successfully.
func (w *Watcher) release(ctx context.Context, iss gh.Issue, unassign bool) {
	ctx = context.WithoutCancel(ctx)
	if err := w.GH.RemoveLabel(ctx, w.Repo, iss.Number, w.Claim); err != nil {
		w.logf("#%d: removing claim label: %v", iss.Number, err)
		if w.wasCancelled(iss.Number) {
			w.recordLifecycleError(err)
		}
	}
	if unassign {
		if err := w.GH.Unassign(ctx, w.Repo, iss.Number); err != nil {
			w.logf("#%d: unassigning: %v", iss.Number, err)
			if w.wasCancelled(iss.Number) {
				w.recordLifecycleError(err)
			}
		}
	}
}
