// Package watch polls a repo for trigger-labelled issues and runs each in a
// worktree at a bounded width until a signal stops it.
package watch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/runner"
)

// GHOps is the GitHub surface watch needs: list labelled issues and move
// labels on and off them.
type GHOps interface {
	ListIssues(ctx context.Context, repo, label string) ([]gh.Issue, error)
	AddLabel(ctx context.Context, repo string, number int, label string) error
	RemoveLabel(ctx context.Context, repo string, number int, label string) error
}

// Store is the job-table surface watch needs.
type Store interface {
	Claim(ctx context.Context, repo string, issue int, branch string) (bool, error)
	Delete(ctx context.Context, repo string, issue int) error
	ClearRunning(ctx context.Context) error
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
	RunJob func(ctx context.Context, issue int) error
	Logf   func(format string, a ...any)
}

func (w *Watcher) logf(format string, a ...any) {
	if w.Logf != nil {
		w.Logf(format, a...)
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", a...)
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

	if err := w.Store.ClearRunning(ctx); err != nil {
		return fmt.Errorf("clearing stale jobs: %w", err)
	}

	jobCtx, cancelAll := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelAll()

	slots := make(chan struct{}, w.Width)
	var wg sync.WaitGroup

	w.logf("watching label %q every %s (width %d)", w.Trigger, w.Interval, w.Width)

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
		select {
		case slots <- struct{}{}:
			if !w.claim(ctx, iss) {
				<-slots
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
	branch := fmt.Sprintf("romp-%d", iss.Number)
	ok, err := w.Store.Claim(ctx, w.Repo, iss.Number, branch)
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
	w.logf("claimed #%d", iss.Number)
	return true
}

func (w *Watcher) runJob(ctx context.Context, iss gh.Issue, slots chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() { <-slots }()
	defer w.release(ctx, iss)

	err := w.RunJob(ctx, iss.Number)
	switch {
	case err == nil:
		if rerr := w.GH.RemoveLabel(ctx, w.Repo, iss.Number, w.Trigger); rerr != nil {
			w.logf("#%d: removing trigger label: %v", iss.Number, rerr)
		}
		w.logf("#%d: done", iss.Number)
	case errors.Is(err, runner.ErrBlocked):
		w.logf("#%d: blocked", iss.Number)
	default:
		w.logf("#%d: %v", iss.Number, err)
	}
}

// release drops the claim label and in-flight row on terminal state, using a
// context that survives cancellation so a force-killed job still unclaims.
func (w *Watcher) release(ctx context.Context, iss gh.Issue) {
	ctx = context.WithoutCancel(ctx)
	if err := w.GH.RemoveLabel(ctx, w.Repo, iss.Number, w.Claim); err != nil {
		w.logf("#%d: removing claim label: %v", iss.Number, err)
	}
	if err := w.Store.Delete(ctx, w.Repo, iss.Number); err != nil {
		w.logf("#%d: deleting job row: %v", iss.Number, err)
	}
}
