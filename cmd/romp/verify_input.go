package main

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/BRO3886/romp/internal/config"
)

type verifyInputModel struct {
	input      textinput.Model
	candidates []config.Candidate
	selected   []string
	done       bool
	aborted    bool
	message    string
}

func chooseVerifyCommands(in io.Reader, out io.Writer, candidates []config.Candidate, initial []string) ([]string, error) {
	initial, err := normalizeVerifyCommands(initial)
	if err != nil {
		return nil, err
	}
	input := textinput.New()
	input.Prompt = "verify> "
	input.Placeholder = "type a command or filter candidates"
	input.ShowSuggestions = true
	input.SetSuggestions(candidateCommands(candidates))
	input.Focus()

	m := verifyInputModel{
		input:      input,
		candidates: candidates,
		selected:   append([]string(nil), initial...),
	}
	model, err := tea.NewProgram(&m, tea.WithInput(in), tea.WithOutput(out)).Run()
	if err != nil {
		return nil, err
	}
	result := model.(*verifyInputModel)
	if result.aborted {
		return nil, fmt.Errorf("verification command selection cancelled")
	}
	return result.selected, nil
}

func normalizeVerifyCommands(commands []string) ([]string, error) {
	normalized := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			return nil, fmt.Errorf("verification command cannot be empty")
		}
		if containsCommand(normalized, command) {
			return nil, fmt.Errorf("duplicate verification command %q", command)
		}
		normalized = append(normalized, command)
	}
	return normalized, nil
}

func (m *verifyInputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *verifyInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var command tea.Cmd
		m.input, command = m.input.Update(msg)
		return m, command
	}

	switch key.String() {
	case "ctrl+c", "esc":
		m.aborted = true
		return m, tea.Quit
	case "enter":
		if strings.TrimSpace(m.input.Value()) == "" {
			m.done = true
			return m, tea.Quit
		}
		command := m.input.Value()
		if suggestion := m.input.CurrentSuggestion(); suggestion != "" && strings.TrimSpace(command) == suggestion {
			command = suggestion
		}
		command = strings.TrimSpace(command)
		if containsCommand(m.selected, command) {
			m.message = "command already selected"
			m.input.Reset()
			return m, nil
		}
		m.selected = append(m.selected, command)
		m.message = ""
		m.input.Reset()
		return m, nil
	}

	var command tea.Cmd
	m.input, command = m.input.Update(msg)
	return m, command
}

func (m *verifyInputModel) View() tea.View {
	var b strings.Builder
	b.WriteString("Verification commands\n")
	b.WriteString("Type to filter or enter a custom command. Tab completes. Enter adds. Empty Enter finishes.\n\n")
	b.WriteString("Selected commands:\n")
	if len(m.selected) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for i, command := range m.selected {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, command)
		}
	}
	b.WriteString("\n")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")
	matched := m.input.MatchedSuggestions()
	if strings.TrimSpace(m.input.Value()) == "" {
		matched = candidateCommands(m.candidates)
	}
	current := m.input.CurrentSuggestion()
	for _, command := range matched {
		prefix := "  "
		if command == current {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%s%-36s %s\n", prefix, command, candidateSource(m.candidates, command))
	}
	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(m.message)
		b.WriteString("\n")
	}
	return tea.NewView(b.String())
}

func candidateCommands(candidates []config.Candidate) []string {
	commands := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		commands = append(commands, candidate.Command)
	}
	return commands
}

func candidateSource(candidates []config.Candidate, command string) string {
	for _, candidate := range candidates {
		if candidate.Command == command {
			return strings.Join(candidate.Sources, ", ")
		}
	}
	return "custom"
}

func containsCommand(commands []string, command string) bool {
	for _, selected := range commands {
		if strings.TrimSpace(selected) == command {
			return true
		}
	}
	return false
}
