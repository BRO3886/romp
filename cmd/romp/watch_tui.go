package main

import (
	"context"
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/progress"
	"github.com/BRO3886/romp/internal/watch"
)

type watchEventMsg progress.Event
type watchTickMsg time.Time
type watchDoneMsg struct{ err error }
type watchHistoryMsg struct {
	outcomes []job.Outcome
	err      error
}

var watchPalette = struct {
	accent, text, muted, border, selected, cyan, amber, violet, green, red color.Color
}{
	accent:   lipgloss.Color("#F4A261"),
	text:     lipgloss.Color("#E8EDF2"),
	muted:    lipgloss.Color("#7D8B99"),
	border:   lipgloss.Color("#33404C"),
	selected: lipgloss.Color("#202C36"),
	cyan:     lipgloss.Color("#62C6E8"),
	amber:    lipgloss.Color("#E9B949"),
	violet:   lipgloss.Color("#B79CED"),
	green:    lipgloss.Color("#75C98D"),
	red:      lipgloss.Color("#EF7D7D"),
}

var (
	watchLogoStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1A2228")).Background(watchPalette.accent).Padding(0, 1)
	watchRepoStyle     = lipgloss.NewStyle().Bold(true).Foreground(watchPalette.text)
	watchMetaStyle     = lipgloss.NewStyle().Foreground(watchPalette.muted)
	watchPanelStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(watchPalette.border).Padding(0, 1)
	watchSelectedStyle = lipgloss.NewStyle().Background(watchPalette.selected)
	watchIssueStyle    = lipgloss.NewStyle().Bold(true).Foreground(watchPalette.text)
	watchTitleStyle    = lipgloss.NewStyle().Foreground(watchPalette.text)
	watchKeyStyle      = lipgloss.NewStyle().Bold(true).Foreground(watchPalette.text).Background(watchPalette.selected).Padding(0, 1)
)

type watchJobView struct {
	issue           int
	title           string
	phase           progress.Phase
	detail          string
	started         time.Time
	events          []progress.Event
	builderHarness  string
	reviewerHarness string
}

type watchTUIModel struct {
	repo       string
	watcher    *watch.Watcher
	store      *job.Store
	events     <-chan progress.Event
	history    []job.Outcome
	active     map[int]*watchJobView
	tab        int
	selected   int
	detail     bool
	draining   bool
	width      int
	height     int
	cancel     context.CancelFunc
	watchCtx   context.Context
	watchError error
}

func runWatchTUI(ctx context.Context, watcher *watch.Watcher, store *job.Store, repo string, events <-chan progress.Event) error {
	history, err := store.History(ctx, repo, 20)
	if err != nil {
		return fmt.Errorf("loading watch history: %w", err)
	}
	watchCtx, cancel := context.WithCancel(ctx)
	m := &watchTUIModel{
		repo: repo, watcher: watcher, store: store, events: events, history: history,
		active: make(map[int]*watchJobView), cancel: cancel, watchCtx: watchCtx,
	}
	defer cancel()
	model, err := tea.NewProgram(m, tea.WithoutSignalHandler()).Run()
	if err != nil {
		return err
	}
	result := model.(*watchTUIModel)
	return result.watchError
}

func (m *watchTUIModel) Init() tea.Cmd {
	return tea.Batch(m.waitEvent(), m.tick(), func() tea.Msg {
		return watchDoneMsg{err: m.watcher.Run(m.watchCtx)}
	})
}

func (m *watchTUIModel) waitEvent() tea.Cmd {
	return func() tea.Msg {
		event, ok := <-m.events
		if !ok {
			return nil
		}
		return watchEventMsg(event)
	}
}

func (m *watchTUIModel) tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return watchTickMsg(t) })
}

func (m *watchTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case watchEventMsg:
		event := progress.Event(msg)
		m.applyEvent(event)
		if event.Terminal {
			return m, tea.Batch(m.waitEvent(), m.loadHistory())
		}
		return m, m.waitEvent()
	case watchHistoryMsg:
		if msg.err != nil {
			m.watchError = msg.err
			return m, tea.Quit
		}
		m.history = msg.outcomes
	case watchTickMsg:
		return m, m.tick()
	case watchDoneMsg:
		m.watchError = msg.err
		return m, tea.Quit
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if !m.draining {
				m.draining = true
				m.cancel()
			}
		case "tab":
			if !m.detail {
				m.tab = (m.tab + 1) % 2
				m.selected = 0
			}
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected+1 < m.rowCount() {
				m.selected++
			}
		case "enter":
			if m.rowCount() > 0 {
				m.detail = true
			}
		case "esc", "backspace":
			m.detail = false
		}
	}
	return m, nil
}

func (m *watchTUIModel) applyEvent(event progress.Event) {
	view := m.active[event.Issue]
	if view == nil {
		view = &watchJobView{issue: event.Issue, started: event.At}
		m.active[event.Issue] = view
	}
	if event.Phase == progress.PhaseClaiming && event.Detail != "" {
		view.title = event.Detail
	}
	if event.BuilderHarness != "" {
		view.builderHarness = event.BuilderHarness
	}
	if event.ReviewerHarness != "" {
		view.reviewerHarness = event.ReviewerHarness
	}
	if event.Phase == "" {
		return
	}
	view.phase = event.Phase
	view.detail = event.Detail
	switch event.Phase {
	case progress.PhaseAgent, progress.PhaseFixing:
		view.builderHarness = event.Harness
	case progress.PhaseReviewing, progress.PhaseRereviewing:
		view.reviewerHarness = event.Harness
	}
	view.events = append(view.events, event)
	if len(view.events) > 50 {
		view.events = append([]progress.Event(nil), view.events[len(view.events)-50:]...)
	}
	if event.Terminal {
		delete(m.active, event.Issue)
	}
	if m.selected >= m.rowCount() && m.selected > 0 {
		m.selected--
	}
}

func (m *watchTUIModel) loadHistory() tea.Cmd {
	return func() tea.Msg {
		outcomes, err := m.store.History(context.WithoutCancel(m.watchCtx), m.repo, 20)
		return watchHistoryMsg{outcomes: outcomes, err: err}
	}
}

func (m *watchTUIModel) rowCount() int {
	if m.tab == 0 {
		return len(m.active)
	}
	return len(m.history)
}

func (m *watchTUIModel) activeRows() []*watchJobView {
	rows := make([]*watchJobView, 0, len(m.active))
	for _, row := range m.active {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].started.Before(rows[j].started) })
	return rows
}

func (m *watchTUIModel) View() tea.View {
	var b strings.Builder
	m.writeHeader(&b)
	if m.detail {
		m.writeDetail(&b)
	} else {
		m.writeDashboard(&b)
	}
	view := tea.NewView(b.String())
	view.AltScreen = true
	view.WindowTitle = "Romp watch"
	return view
}

func (m *watchTUIModel) writeHeader(b *strings.Builder) {
	state := lipgloss.NewStyle().Bold(true).Foreground(watchPalette.green).Render("● WATCHING")
	if m.draining {
		state = lipgloss.NewStyle().Bold(true).Foreground(watchPalette.amber).Render("◌ DRAINING")
	}
	active := watchMetaStyle.Render(fmt.Sprintf("%d active", len(m.active)))
	fmt.Fprintf(b, "%s  %s  %s  %s\n\n", watchLogoStyle.Render("ROMP"), watchRepoStyle.Render(m.repo), active, state)
}

func (m *watchTUIModel) writeDashboard(b *strings.Builder) {
	b.WriteString(m.renderTabs())
	b.WriteString("\n")
	var panel strings.Builder
	if m.tab == 0 {
		rows := m.activeRows()
		if len(rows) == 0 {
			panel.WriteString("\n  " + watchMetaStyle.Render("◌") + "  " + watchTitleStyle.Render("No active jobs") + "\n")
			panel.WriteString("     " + watchMetaStyle.Render("Watching for issues with the trigger label") + "\n\n")
		}
		start, end := visibleRange(len(rows), m.selected, max(1, (m.contentHeight()-3)/3))
		for i := start; i < end; i++ {
			panel.WriteString(m.renderActiveRow(rows[i], i == m.selected))
		}
	} else {
		if len(m.history) == 0 {
			panel.WriteString("\n  " + watchMetaStyle.Render("◌") + "  " + watchTitleStyle.Render("No finished jobs yet") + "\n\n")
		}
		start, end := visibleRange(len(m.history), m.selected, max(1, m.contentHeight()-3))
		for i := start; i < end; i++ {
			panel.WriteString(m.renderHistoryRow(m.history[i], i == m.selected))
		}
	}
	width := max(40, m.width-2)
	b.WriteString(watchPanelStyle.Width(width - 4).Render(panel.String()))
	b.WriteString("\n\n")
	m.writeFooter(b, false)
	if m.draining {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(watchPalette.amber).Render("◌ Finishing running jobs. Press Ctrl-C again to stop them."))
	}
}

func (m *watchTUIModel) renderTabs() string {
	active := lipgloss.NewStyle().Foreground(watchPalette.muted).Padding(0, 1)
	history := active
	if m.tab == 0 {
		active = active.Bold(true).Foreground(watchPalette.accent).BorderBottom(true).BorderForeground(watchPalette.accent)
	} else {
		history = history.Bold(true).Foreground(watchPalette.accent).BorderBottom(true).BorderForeground(watchPalette.accent)
	}
	return active.Render(fmt.Sprintf("Active %d", len(m.active))) + "  " + history.Render(fmt.Sprintf("History %d", len(m.history)))
}

func (m *watchTUIModel) renderActiveRow(row *watchJobView, selected bool) string {
	icon, color := phaseAppearance(row.phase)
	phase := lipgloss.NewStyle().Bold(true).Foreground(color).Render(icon + " " + strings.ToUpper(string(row.phase)))
	elapsed := watchMetaStyle.Render(time.Since(row.started).Round(time.Second).String())
	line1 := fmt.Sprintf("%s  %s  %s  %s  %s", selectedGlyph(selected), watchIssueStyle.Render(fmt.Sprintf("#%d", row.issue)), phase, activeHarnessLabel(row), elapsed)
	available := max(20, m.width-12)
	title := watchTitleStyle.Render(clipText(valueOr(row.title, "Untitled issue"), available))
	detail := watchMetaStyle.Render(clipText(row.detail, available))
	content := line1 + "\n   " + title + "\n   " + detail + "\n"
	if selected {
		return watchSelectedStyle.Width(max(36, m.width-8)).Render(content) + "\n"
	}
	return content + "\n"
}

func (m *watchTUIModel) renderHistoryRow(outcome job.Outcome, selected bool) string {
	icon, color := outcomeAppearance(outcome.Outcome)
	status := lipgloss.NewStyle().Bold(true).Foreground(color).Render(icon + " " + strings.ToUpper(outcome.Outcome))
	when := watchMetaStyle.Render(compactTime(outcome.FinishedAt))
	content := fmt.Sprintf("%s  %s  %-22s  %-18s  %s", selectedGlyph(selected), watchIssueStyle.Render(fmt.Sprintf("#%d", outcome.Issue)), status, historyHarnessLabel(outcome), when)
	if selected {
		return watchSelectedStyle.Width(max(36, m.width-8)).Render(content) + "\n"
	}
	return content + "\n"
}

func (m *watchTUIModel) writeDetail(b *strings.Builder) {
	var panel strings.Builder
	if m.tab == 0 {
		rows := m.activeRows()
		if m.selected >= len(rows) {
			return
		}
		row := rows[m.selected]
		icon, color := phaseAppearance(row.phase)
		fmt.Fprintf(&panel, "%s  %s\n", watchIssueStyle.Render(fmt.Sprintf("#%d", row.issue)), lipgloss.NewStyle().Bold(true).Foreground(color).Render(icon+" "+strings.ToUpper(string(row.phase))))
		panel.WriteString(watchTitleStyle.Render(row.title) + "\n\n")
		start := max(0, len(row.events)-max(1, m.contentHeight()-6))
		for i, event := range row.events[start:] {
			eventIcon, eventColor := phaseAppearance(event.Phase)
			connector := "├"
			if i == len(row.events[start:])-1 {
				connector = "└"
			}
			fmt.Fprintf(&panel, "%s %s  %s  %s\n", watchMetaStyle.Render(connector), lipgloss.NewStyle().Foreground(eventColor).Render(eventIcon), watchMetaStyle.Render(event.At.Format("15:04:05")), clipText(event.Detail, max(20, m.width-30)))
		}
	} else if m.selected < len(m.history) {
		o := m.history[m.selected]
		icon, color := outcomeAppearance(o.Outcome)
		fmt.Fprintf(&panel, "%s  %s\n\n", watchIssueStyle.Render(fmt.Sprintf("#%d", o.Issue)), lipgloss.NewStyle().Bold(true).Foreground(color).Render(icon+" "+strings.ToUpper(o.Outcome)))
		writeDetailField(&panel, "Started", compactTime(o.StartedAt))
		writeDetailField(&panel, "Finished", compactTime(o.FinishedAt))
		writeDetailField(&panel, "Pull request", valueOrDash(o.PRURL))
		writeDetailField(&panel, "Session", valueOrDash(o.SessionID))
		writeDetailField(&panel, "Builder", valueOr(o.BuilderHarness, "—"))
		writeDetailField(&panel, "Reviewer", valueOr(o.ReviewerHarness, "disabled or unknown"))
		if o.Detail != "" {
			panel.WriteString("\n" + lipgloss.NewStyle().Foreground(color).Render(clipText(o.Detail, max(30, m.width-12))) + "\n")
		}
	}
	b.WriteString(watchPanelStyle.Width(max(36, m.width-6)).Render(panel.String()))
	b.WriteString("\n\n")
	m.writeFooter(b, true)
}

func (m *watchTUIModel) writeFooter(b *strings.Builder, detail bool) {
	if detail {
		fmt.Fprintf(b, "%s back   %s drain and quit", watchKeyStyle.Render("esc"), watchKeyStyle.Render("q"))
		return
	}
	fmt.Fprintf(b, "%s switch   %s navigate   %s inspect   %s drain", watchKeyStyle.Render("tab"), watchKeyStyle.Render("↑/↓"), watchKeyStyle.Render("enter"), watchKeyStyle.Render("q"))
}

func (m *watchTUIModel) contentHeight() int {
	if m.height <= 0 {
		return 12
	}
	return max(4, m.height-9)
}

func phaseAppearance(phase progress.Phase) (string, color.Color) {
	switch phase {
	case progress.PhaseAgent, progress.PhaseFixing:
		return "◆", watchPalette.cyan
	case progress.PhaseVerifying, progress.PhaseReverifying:
		return "◇", watchPalette.amber
	case progress.PhaseReviewing, progress.PhaseRereviewing:
		return "◈", watchPalette.violet
	case progress.PhasePublishing:
		return "↑", watchPalette.accent
	case progress.PhaseDone:
		return "✓", watchPalette.green
	case progress.PhaseFailed:
		return "×", watchPalette.red
	default:
		return "●", watchPalette.muted
	}
}

func outcomeAppearance(outcome string) (string, color.Color) {
	switch outcome {
	case "done":
		return "✓", watchPalette.green
	case "blocked", "changes-requested":
		return "!", watchPalette.amber
	default:
		return "×", watchPalette.red
	}
}

func selectedGlyph(selected bool) string {
	if selected {
		return lipgloss.NewStyle().Bold(true).Foreground(watchPalette.accent).Render("▸")
	}
	return " "
}

func writeDetailField(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "%s  %s\n", watchMetaStyle.Width(12).Render(label), watchTitleStyle.Render(value))
}

func compactTime(value string) string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return valueOr(value, "—")
	}
	return t.Local().Format("02 Jan  15:04")
}

func visibleRange(total, selected, limit int) (int, int) {
	if total <= limit {
		return 0, total
	}
	start := selected - limit/2
	start = max(0, min(start, total-limit))
	return start, start + limit
}

func clipText(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	var b strings.Builder
	for _, r := range value {
		if lipgloss.Width(b.String()+string(r)+"…") > width {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func activeHarnessLabel(row *watchJobView) string {
	if row.builderHarness == "" {
		return watchMetaStyle.Render("—")
	}
	builder := lipgloss.NewStyle().Foreground(watchPalette.cyan)
	reviewer := lipgloss.NewStyle().Foreground(watchPalette.violet)
	if row.phase == progress.PhaseAgent || row.phase == progress.PhaseFixing {
		builder = builder.Bold(true)
	}
	if row.phase == progress.PhaseReviewing || row.phase == progress.PhaseRereviewing {
		reviewer = reviewer.Bold(true)
	}
	label := builder.Render(strings.ToUpper(row.builderHarness))
	if row.reviewerHarness != "" {
		label += watchMetaStyle.Render(" → ") + reviewer.Render(strings.ToUpper(row.reviewerHarness))
	}
	return label
}

func historyHarnessLabel(outcome job.Outcome) string {
	value := valueOr(outcome.BuilderHarness, "—")
	if outcome.ReviewerHarness != "" {
		value += " → " + outcome.ReviewerHarness
	}
	return watchMetaStyle.Render(value)
}
