package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/BRO3886/romp/internal/config"
)

func TestNormalizeVerifyCommands(t *testing.T) {
	got, err := normalizeVerifyCommands([]string{" make test ", "make lint"})
	if err != nil {
		t.Fatalf("normalizeVerifyCommands: %v", err)
	}
	if len(got) != 2 || got[0] != "make test" || got[1] != "make lint" {
		t.Errorf("normalized = %v, want trimmed ordered commands", got)
	}
	if _, err := normalizeVerifyCommands([]string{"make test", " make test "}); err == nil {
		t.Fatal("normalizeVerifyCommands duplicate = nil error")
	}
}

func TestVerifyInputAddsTypedCommandAndFinishesOnEmptyEnter(t *testing.T) {
	m := newVerifyInputModel(nil, nil)
	m.input.SetValue("make dragon")

	if _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); len(m.selected) != 1 || m.selected[0] != "make dragon" {
		t.Fatalf("selected = %v, want typed command", m.selected)
	}
	if _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); !m.done {
		t.Fatal("empty Enter did not finish")
	}
}

func TestVerifyInputPrefersTypedCommandOverPrefixSuggestion(t *testing.T) {
	m := newVerifyInputModel([]config.Candidate{{Command: "make test"}}, nil)
	m.input.SetValue("make t")
	m.syncCandidateFilter()

	if _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); len(m.selected) != 1 || m.selected[0] != "make t" {
		t.Fatalf("selected = %v, want typed command", m.selected)
	}
}

func TestVerifyInputViewShowsCandidatesAndSources(t *testing.T) {
	m := newVerifyInputModel(
		[]config.Candidate{{Command: "make dragon", Sources: []string{"Makefile target \"dragon\""}}},
		nil,
	)
	view := m.View().Content
	if !strings.Contains(view, "make dragon") || !strings.Contains(view, "Makefile target \"dragon\"") {
		t.Errorf("view = %q, want candidate and source", view)
	}
}

func TestVerifyInputUpFromFirstCandidateRendersWithoutPanic(t *testing.T) {
	candidates := []config.Candidate{
		{Command: "make test", Sources: []string{"Makefile target \"test\""}},
		{Command: "make lint", Sources: []string{"Makefile target \"lint\""}},
	}
	m := newVerifyInputModel(candidates, nil)

	if _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp}); m.View().Content == "" {
		t.Fatal("view is empty after Up from the first candidate")
	}
}

func TestVerifyInputNavigationStaysWithinCandidateBounds(t *testing.T) {
	m := newVerifyInputModel([]config.Candidate{
		{Command: "make test"},
		{Command: "make lint"},
	}, nil)

	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := selectedCandidateCommand(t, m); got != "make test" {
		t.Errorf("selected after Up = %q, want first candidate", got)
	}

	for range 3 {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if got := selectedCandidateCommand(t, m); got != "make lint" {
		t.Errorf("selected after Down past end = %q, want last candidate", got)
	}
}

func TestVerifyInputFiltersAndSelectsDiscoveredCommand(t *testing.T) {
	m := newVerifyInputModel([]config.Candidate{
		{Command: "make test", Sources: []string{"Makefile target \"test\""}},
		{Command: "make lint", Sources: []string{"Makefile target \"lint\""}},
	}, nil)

	typeVerifyInput(&m, "lint")
	if len(m.candidateList.VisibleItems()) != 1 {
		t.Fatalf("visible candidates = %d, want 1", len(m.candidateList.VisibleItems()))
	}
	if got := selectedCandidateCommand(t, m); got != "make lint" {
		t.Fatalf("filtered candidate = %q, want make lint", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.selected) != 1 || m.selected[0] != "make lint" {
		t.Fatalf("selected commands = %v, want [make lint]", m.selected)
	}
}

func TestVerifyInputSelectsCustomCommand(t *testing.T) {
	m := newVerifyInputModel([]config.Candidate{{Command: "make test"}}, nil)

	typeVerifyInput(&m, "make dragon")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.selected) != 1 || m.selected[0] != "make dragon" {
		t.Fatalf("selected commands = %v, want [make dragon]", m.selected)
	}
}

func TestVerifyInputSelectsMultipleCommandsRejectsDuplicateAndFinishes(t *testing.T) {
	m := newVerifyInputModel([]config.Candidate{{Command: "make test"}}, nil)

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	typeVerifyInput(&m, "make dragon")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	typeVerifyInput(&m, "make test")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(m.selected) != 2 || m.selected[0] != "make test" || m.selected[1] != "make dragon" {
		t.Fatalf("selected commands = %v, want [make test make dragon]", m.selected)
	}
	if m.message != "command already selected" {
		t.Errorf("duplicate message = %q, want command already selected", m.message)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.done {
		t.Fatal("empty Enter did not finish")
	}
}

func TestVerifyInputCancels(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "Escape", key: tea.KeyPressMsg{Code: tea.KeyEscape}},
		{name: "Ctrl+C", key: tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newVerifyInputModel([]config.Candidate{{Command: "make test"}}, nil)
			m.Update(tt.key)
			if !m.aborted {
				t.Fatal("input was not cancelled")
			}
		})
	}
}

func selectedCandidateCommand(t *testing.T, m verifyInputModel) string {
	t.Helper()
	item, ok := m.candidateList.SelectedItem().(verifyCandidateItem)
	if !ok {
		t.Fatal("selected candidate is missing")
	}
	return item.command
}

func typeVerifyInput(m *verifyInputModel, value string) {
	for _, r := range value {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}
