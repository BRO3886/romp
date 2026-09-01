// Package watch polls a repo for trigger-labelled issues and runs each in a
// worktree at a bounded width until a signal stops it.
package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/job"
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
	ClearRunning(ctx context.Context, repo string) error
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

	GH     GHOps
	Store  Store
	RunJob func(ctx context.Context, issue int) (string, error)
	// CleanJob removes a cancelled job's worktree and branch. It is the
	// runner's counterpart, invoked only for an abandon so the watcher does
	// not need git access.
	CleanJob func(ctx context.Context, issue int) error
	Logf     func(format string, a ...any)
	Stderr   io.Writer

	mu        sync.Mutex
	cancels   map[int]context.CancelFunc
	cancelled map[int]bool
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

// Run clears stale in-flight rows, then polls and dispatches until the first
// signal (graceful drain) or a parent cancellation. A second signal cancels
// running jobs immediately.
func (w *Watcher) Run(ctx context.Context) error {
	if w.Width < 1 {
		w.Width = 1
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if err := w.Store.ClearRunning(ctx, w.Repo); err != nil {
		return fmt.Errorf("clearing stale jobs: %w", err)
	}

	jobCtx, cancelAll := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelAll()

	slots := make(chan struct{}, w.Width)
	for i := 0; i < w.Width; i++ {
		slots <- struct{}{}
	}
	var wg sync.WaitGroup

	w.logf("watching label %q every %s (width %d)", w.Trigger, w.Interval, w.Width)

	if ln, err := listen(w.Repo); err != nil {
		w.logf("cancel socket: %v", err)
	} else {
		defer ln.Close()
		go w.serve(ln)
	}

	if err := w.claimBatch(ctx, slots, &wg, jobCtx); err != nil {
		w.logf("poll: %v", err)
	}

	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	for {
		select {
		case s := <-sigCh:
			w.logf("received %v; finishing running jobs, no new claims", s)
			return w.drain(sigCh, &wg, cancelAll)
		case <-ctx.Done():
			return w.drain(sigCh, &wg, cancelAll)
		case <-ticker.C:
			if err := w.claimBatch(ctx, slots, &wg, jobCtx); err != nil {
				w.logf("poll: %v", err)
			}
		}
	}
}

func (w *Watcher) drain(sigCh <-chan os.Signal, wg *sync.WaitGroup, cancelAll context.CancelFunc) error {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-sigCh:
		w.logf("second interrupt; killing running jobs")
		cancelAll()
		<-done
		return nil
	}
}

// claimBatch lists trigger-labelled issues and dispatches each unclaimed one,
// stopping early when every slot is busy so the leftover issues stay unclaimed
// for another watcher (or the next tick).
func (w *Watcher) claimBatch(ctx context.Context, slots chan struct{}, wg *sync.WaitGroup, jobCtx context.Context) error {
	issues, err := w.GH.ListIssues(ctx, w.Repo, w.Trigger)
	if err != nil {
		return err
	}
	for _, iss := range issues {
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
			if !w.claim(ctx, iss) {
				slots <- struct{}{}
				continue
			}
			wg.Add(1)
			go w.runJob(jobCtx, iss, slots, wg)
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
	}
	w.cancels[issue] = cancel
}

func (w *Watcher) unregister(issue int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.cancels, issue)
	delete(w.cancelled, issue)
}

// cancelIssue kills the job for issue and records it as cancelled, reporting
// whether such a job was running.
func (w *Watcher) cancelIssue(issue int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	cancel, ok := w.cancels[issue]
	if !ok {
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

// serve accepts control-socket connections until the listener is closed.
func (w *Watcher) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			w.logf("cancel socket: accept: %v", err)
			continue
		}
		go w.handleConn(conn)
	}
}

func (w *Watcher) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	var req CancelRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(CancelResponse{Error: "invalid request"})
		return
	}
	if req.Issue <= 0 {
		_ = json.NewEncoder(conn).Encode(CancelResponse{Error: "invalid issue number"})
		return
	}
	if !w.cancelIssue(req.Issue) {
		_ = json.NewEncoder(conn).Encode(CancelResponse{Error: fmt.Sprintf("no running job for issue %d", req.Issue)})
		return
	}
	w.logf("cancelled #%d", req.Issue)
	_ = json.NewEncoder(conn).Encode(CancelResponse{OK: true})
}

func (w *Watcher) runJob(ctx context.Context, iss gh.Issue, slots chan<- struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() { slots <- struct{}{} }()
	unassign := true
	defer func() { w.release(ctx, iss, unassign) }()

	runCtx, cancel := context.WithCancel(ctx)
	w.register(iss.Number, cancel)
	defer w.unregister(iss.Number)

	prURL, err := w.RunJob(runCtx, iss.Number)
	cancelled := w.wasCancelled(iss.Number)
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
	}

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
		}
	}

	// Abandon (ADR 0009): a cancelled job is dropped, not retried, so its
	// worktree and branch are cleaned up.
	if cancelled {
		if w.CleanJob != nil {
			if err := w.CleanJob(context.WithoutCancel(ctx), iss.Number); err != nil {
				w.logf("#%d: cleaning up job: %v", iss.Number, err)
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
	}
	if unassign {
		if err := w.GH.Unassign(ctx, w.Repo, iss.Number); err != nil {
			w.logf("#%d: unassigning: %v", iss.Number, err)
		}
	}
	if err := w.Store.Delete(ctx, w.Repo, iss.Number); err != nil {
		w.logf("#%d: deleting job row: %v", iss.Number, err)
	}
}
