package watch

import (
	"context"
	"fmt"
	"testing"

	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/job"
)

type pausedFinish struct {
	*fakeStore
	entered, proceed chan struct{}
}

func (s pausedFinish) Finish(ctx context.Context, o job.Outcome) error {
	close(s.entered)
	<-s.proceed
	return s.fakeStore.Finish(ctx, o)
}

func TestCancelRefusesAlreadyTerminalExecution(t *testing.T) {
	store := pausedFinish{newFakeStore(), make(chan struct{}), make(chan struct{})}
	w := &Watcher{Repo: "o/r", Width: 1, GH: &fakeGH{issues: []gh.Issue{{Number: 1}}}, Store: store, RunJob: func(context.Context, int) (string, error) { return "", nil }}
	admitted, err := w.PollOne(context.Background())
	if err != nil || !admitted {
		t.Fatal(admitted, err)
	}
	<-store.entered
	accepted := w.Cancel(1)
	close(store.proceed)
	if err := w.Wait(); err != nil {
		t.Fatal(err)
	}
	if accepted {
		t.Fatal("cancel reported success after the terminal outcome was selected")
	}
	if len(store.finished) != 1 || store.finished[0].Outcome != "done" {
		t.Fatalf("terminal outcome=%+v", store.finished)
	}
}

type finishFailure struct{ *fakeStore }

func (s finishFailure) Finish(context.Context, job.Outcome) error { return fmt.Errorf("disk full") }

func TestFinishFailurePreservesJobRow(t *testing.T) {
	store := newFakeStore()
	store.rows[7] = true
	w := &Watcher{Repo: "o/r", GH: &fakeGH{}, Store: finishFailure{store}, RunJob: func(context.Context, int) (string, error) { return "", nil }}
	w.runJobSync(t, 7)
	if !store.has(7) {
		t.Fatal("history write failed but active row was deleted; crash recovery cannot record this job")
	}
}

func TestPollOneCancelBeforeWorkerStarts(t *testing.T) {
	store := newFakeStore()
	run := make(chan struct{})
	cleaned := false
	w := &Watcher{Repo: "o/r", Trigger: "romp", Claim: "romp:claimed", Width: 3, GH: &fakeGH{issues: []gh.Issue{{Number: 1}, {Number: 2}}}, Store: store}
	w.RunJob = func(ctx context.Context, issue int) (string, error) { close(run); <-ctx.Done(); return "", ctx.Err() }
	w.CleanJob = func(context.Context, int) error { cleaned = true; return nil }
	admitted, err := w.PollOne(context.Background())
	if err != nil || !admitted {
		t.Fatalf("admitted=%v err=%v", admitted, err)
	}
	if !w.Cancel(1) {
		t.Fatal("cancel did not find synchronously registered job")
	}
	if err := w.Wait(); err != nil {
		t.Fatal(err)
	}
	<-run
	if !cleaned || w.Active() != 0 {
		t.Fatalf("cleanup=%v active=%d", cleaned, w.Active())
	}
	if len(store.finished) != 1 || store.finished[0].Outcome != "cancelled" {
		t.Fatalf("outcomes=%+v", store.finished)
	}
	if store.has(2) {
		t.Fatal("one poll tick admitted a second job")
	}
}
