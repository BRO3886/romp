package main

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
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
	input := textinput.New()
	input.Focus()
	input.SetValue("make dragon")
	m := &verifyInputModel{input: input}

	if _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); len(m.selected) != 1 || m.selected[0] != "make dragon" {
		t.Fatalf("selected = %v, want typed command", m.selected)
	}
	if _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); !m.done {
		t.Fatal("empty Enter did not finish")
	}
}

func TestVerifyInputPrefersTypedCommandOverPrefixSuggestion(t *testing.T) {
	input := textinput.New()
	input.ShowSuggestions = true
	input.SetSuggestions([]string{"make test"})
	input.SetValue("make t")
	input.Focus()
	m := &verifyInputModel{input: input}

	if _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); len(m.selected) != 1 || m.selected[0] != "make t" {
		t.Fatalf("selected = %v, want typed command", m.selected)
	}
}

func TestVerifyInputViewShowsCandidatesAndSources(t *testing.T) {
	input := textinput.New()
	input.Focus()
	m := &verifyInputModel{
		input:      input,
		candidates: []config.Candidate{{Command: "make dragon", Sources: []string{"Makefile target \"dragon\""}}},
	}
	view := m.View().Content
	if !strings.Contains(view, "make dragon") || !strings.Contains(view, "Makefile target \"dragon\"") {
		t.Errorf("view = %q, want candidate and source", view)
	}
}
