package main

import (
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
	if len(m.history) != 1 || m.history[0].Issue != 7 || m.history[0].Outcome != "done" || m.history[0].BuilderHarness != "codex" || m.history[0].ReviewerHarness != "claude" {
		t.Fatalf("history = %+v", m.history)
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
			7: {issue: 7, title: "Improve watch output", phase: progress.PhaseReviewing, detail: "reviewer working (read-only)", reviewerHarness: "codex", started: now},
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
