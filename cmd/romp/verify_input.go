package main

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/BRO3886/romp/internal/config"
)

type verifyInputModel struct {
	input         textinput.Model
	candidateList list.Model
	selected      []string
	done          bool
	aborted       bool
	message       string
}

type verifyCandidateItem struct {
	command string
	sources []string
}

func (i verifyCandidateItem) FilterValue() string {
	return i.command
}

func (i verifyCandidateItem) Title() string {
	return i.command
}

func (i verifyCandidateItem) Description() string {
	return strings.Join(i.sources, ", ")
}

func chooseVerifyCommands(in io.Reader, out io.Writer, candidates []config.Candidate, initial []string) ([]string, error) {
	initial, err := normalizeVerifyCommands(initial)
	if err != nil {
		return nil, err
	}
	m := newVerifyInputModel(candidates, initial)
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

func newVerifyInputModel(candidates []config.Candidate, initial []string) verifyInputModel {
	input := textinput.New()
	input.Prompt = "verify> "
	input.Placeholder = "type a command or filter candidates"
	input.Focus()

	items := make([]list.Item, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, verifyCandidateItem{
			command: candidate.Command,
			sources: append([]string(nil), candidate.Sources...),
		})
	}
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	candidateList := list.New(items, delegate, 80, 12)
	candidateList.Title = "Discovered commands"
	candidateList.Filter = list.UnsortedFilter
	candidateList.SetShowFilter(false)
	candidateList.SetShowStatusBar(false)
	candidateList.SetShowHelp(false)
	candidateList.DisableQuitKeybindings()

	return verifyInputModel{
		input:         input,
		candidateList: candidateList,
		selected:      append([]string(nil), initial...),
	}
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
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.input.SetWidth(size.Width)
			m.candidateList.SetSize(size.Width, max(3, size.Height-10))
		}
		var inputCmd, listCmd tea.Cmd
		m.input, inputCmd = m.input.Update(msg)
		m.candidateList, listCmd = m.candidateList.Update(msg)
		return m, tea.Batch(inputCmd, listCmd)
	}

	switch key.String() {
	case "ctrl+c", "esc":
		m.aborted = true
		return m, tea.Quit
	case "up", "down":
		var command tea.Cmd
		m.candidateList, command = m.candidateList.Update(msg)
		return m, command
	case "tab":
		if candidate, ok := m.candidateList.SelectedItem().(verifyCandidateItem); ok {
			m.input.SetValue(candidate.command)
			m.syncCandidateFilter()
		}
		return m, nil
	case "enter":
		if strings.TrimSpace(m.input.Value()) == "" {
			m.done = true
			return m, tea.Quit
		}
		command := strings.TrimSpace(m.input.Value())
		if candidate, ok := m.candidateList.SelectedItem().(verifyCandidateItem); ok && command == candidate.command {
			command = candidate.command
		}
		if containsCommand(m.selected, command) {
			m.message = "command already selected"
			m.resetInput()
			return m, nil
		}
		m.selected = append(m.selected, command)
		m.message = ""
		m.resetInput()
		return m, nil
	}

	var command tea.Cmd
	m.input, command = m.input.Update(msg)
	m.syncCandidateFilter()
	return m, command
}

func (m *verifyInputModel) syncCandidateFilter() {
	filter := strings.TrimSpace(m.input.Value())
	if filter == "" {
		m.candidateList.ResetFilter()
		m.candidateList.ResetSelected()
		return
	}
	m.candidateList.SetFilterText(filter)
}

func (m *verifyInputModel) resetInput() {
	m.input.Reset()
	m.candidateList.ResetFilter()
	m.candidateList.ResetSelected()
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
	b.WriteString(m.candidateList.View())
	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(m.message)
		b.WriteString("\n")
	}
	return tea.NewView(b.String())
}

func containsCommand(commands []string, command string) bool {
	for _, selected := range commands {
		if strings.TrimSpace(selected) == command {
			return true
		}
	}
	return false
}
