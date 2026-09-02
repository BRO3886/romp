package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/progress"
)

func TestWatchTUITracksPhaseAndMovesTerminalJobToHistory(t *testing.T) {
	started := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	m := &watchTUIModel{repo: "o/r", active: make(map[int]*watchJobView)}

	m.applyEvent(progress.Event{Issue: 7, Phase: progress.PhaseClaiming, Detail: "Improve watch output", At: started})
	m.applyEvent(progress.Event{Issue: 7, Phase: progress.PhaseAgent, Detail: "agent working", Harness: "codex", At: started.Add(30 * time.Second)})
	m.applyEvent(progress.Event{Issue: 7, Phase: progress.PhaseReviewing, Detail: "reviewer working (read-only)", Harness: "claude", At: started.Add(time.Minute)})

	active := m.active[7]
	if active == nil || active.phase != progress.PhaseReviewing || active.title != "Improve watch output" || active.builderHarness != "codex" || active.reviewerHarness != "claude" {
		t.Fatalf("active job = %+v", active)
	}

	m.applyEvent(progress.Event{Issue: 7, Phase: progress.PhaseDone, Outcome: "done", At: started.Add(2 * time.Minute), Terminal: true})
	if m.active[7] != nil {
		t.Fatal("terminal job remains active")
	}
	m.Update(watchHistoryMsg{outcomes: []job.Outcome{{Repo: "o/r", Issue: 7, Outcome: "done", BuilderHarness: "codex", ReviewerHarness: "claude"}}})
	if len(m.history) != 1 || m.history[0].Issue != 7 || m.history[0].Outcome != "done" || m.history[0].BuilderHarness != "codex" || m.history[0].ReviewerHarness != "claude" {
		t.Fatalf("history = %+v", m.history)
	}
}

func TestWatchTUIShowsConfiguredReviewerBeforeReviewStarts(t *testing.T) {
	m := &watchTUIModel{active: make(map[int]*watchJobView)}
	m.applyEvent(progress.Event{Issue: 7, BuilderHarness: "codex", ReviewerHarness: "claude", At: time.Now()})
	m.applyEvent(progress.Event{Issue: 7, Phase: progress.PhaseAgent, Detail: "agent working", Harness: "codex", At: time.Now()})

	active := m.active[7]
	if active.builderHarness != "codex" || active.reviewerHarness != "claude" {
		t.Fatalf("configured harnesses = %q/%q, want codex/claude", active.builderHarness, active.reviewerHarness)
	}
	view := m.View().Content
	if !strings.Contains(view, "CODEX") || !strings.Contains(view, "CLAUDE") {
		t.Fatalf("active view does not show configured harnesses:\n%s", view)
	}
}

func TestWatchTUITerminalEventDoesNotCreatePartialHistory(t *testing.T) {
	m := &watchTUIModel{active: map[int]*watchJobView{7: {issue: 7, started: time.Now()}}}
	m.applyEvent(progress.Event{Issue: 7, Phase: progress.PhaseDone, Outcome: "done", Terminal: true, At: time.Now()})

	if len(m.history) != 0 {
		t.Fatalf("terminal event created partial history = %+v", m.history)
	}
}

func TestWatchTUILoadsCompleteTerminalHistoryFromStore(t *testing.T) {
	store, err := job.Open(filepath.Join(t.TempDir(), "romp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	if ok, err := store.Claim(ctx, "o/r", 7, "romp-7"); err != nil || !ok {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	if err := store.SetSessionID(ctx, "o/r", 7, "session-7"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHarnesses(ctx, "o/r", 7, "codex", "claude"); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, job.Outcome{Repo: "o/r", Issue: 7, Outcome: "done", PRURL: "https://example.test/pr/7", FinishedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	m := &watchTUIModel{repo: "o/r", store: store, watchCtx: ctx, active: make(map[int]*watchJobView)}
	msg := m.loadHistory()().(watchHistoryMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(msg.outcomes) != 1 {
		t.Fatalf("history = %+v", msg.outcomes)
	}
	outcome := msg.outcomes[0]
	if outcome.SessionID != "session-7" || outcome.BuilderHarness != "codex" || outcome.ReviewerHarness != "claude" || outcome.PRURL != "https://example.test/pr/7" {
		t.Fatalf("complete history = %+v", outcome)
	}
}

func TestWatchTUINavigationSwitchesTabsAndDetails(t *testing.T) {
	m := &watchTUIModel{
		active:  map[int]*watchJobView{7: {issue: 7, started: time.Now()}},
		history: []job.Outcome{{Issue: 6, Outcome: "done"}},
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.tab != 1 {
		t.Fatalf("tab = %d, want history", m.tab)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.detail || !strings.Contains(m.View().Content, "#6") || !strings.Contains(m.View().Content, "DONE") {
		t.Fatalf("detail view = %q", m.View().Content)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.detail {
		t.Fatal("escape did not return to dashboard")
	}
}

func TestWatchTUIRendersSemanticPhaseStyles(t *testing.T) {
	now := time.Now()
	m := &watchTUIModel{
		repo:   "o/r",
		width:  100,
		height: 30,
		active: map[int]*watchJobView{
			7: {issue: 7, title: "Improve watch output", phase: progress.PhaseReviewing, detail: "reviewer working (read-only)", builderHarness: "codex", reviewerHarness: "codex", started: now},
		},
	}

	view := m.View().Content
	for _, want := range []string{"ROMP", "o/r", "● WATCHING", "Active 1", "◈ REVIEWING", "CODEX", "Improve watch output", "reviewer working (read-only)", "tab", "enter"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "\x1b[") {
		t.Fatal("dashboard has no ANSI styling")
	}
}
