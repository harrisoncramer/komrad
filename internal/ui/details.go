package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type detailsState struct {
	focused bool
	cursor  int

	row string

	flashed int
}

type details struct {
	id string

	kind   string
	title  string
	fields []recordField
}

func (m model) hoveredDetails() details {
	switch m.screen {
	case screenAgents:
		return m.agentsDetails()
	case screenSchedules:
		return m.schedulesDetails()
	case screenArchive:
		return m.archiveDetails()
	default:
		return details{}
	}
}

func (m model) detailsScreen() bool {
	switch m.screen {
	case screenAgents, screenSchedules, screenArchive:
		return true
	default:
		return false
	}
}

func (m model) detailsCursor(held details) int {
	if m.details.row != held.id {
		return 0
	}

	return min(m.details.cursor, max(len(held.fields)-1, 0))
}

func (m model) detailsDrawn(held details) bool {
	return len(held.fields) > 0 && m.detailsLines() > 0
}

func (m model) detailsFocused() bool {
	return m.details.focused && m.detailsDrawn(m.hoveredDetails())
}

func (m model) toggleDetailsFocus() (tea.Model, tea.Cmd) {
	if m.detailsFocused() {
		m.details.focused = false

		return m, nil
	}

	held := m.hoveredDetails()
	if !m.detailsDrawn(held) {
		return m, nil
	}

	m.details.focused = true
	m.details.cursor = 0
	m.details.row = held.id

	return m, nil
}

func (m model) updateDetails(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	held := m.hoveredDetails()

	if !m.detailsDrawn(held) {
		m.details.focused = false

		return m, nil
	}

	cursor := m.detailsCursor(held)

	switch {
	case isBound(k, m.keys.Details.Focus), k == "esc":
		m.details.focused = false
	case isBound(k, m.quitKeys()):
		return m, m.hide()
	case isBound(k, m.keys.Nav.Up):
		m = m.setDetailsCursor(held, cursor-1)
	case isBound(k, m.keys.Nav.Down):
		m = m.setDetailsCursor(held, cursor+1)
	case isBound(k, m.keys.Nav.Top):
		m = m.setDetailsCursor(held, 0)
	case isBound(k, m.keys.Nav.Bottom):
		m = m.setDetailsCursor(held, len(held.fields)-1)
	case isBound(k, m.keys.Details.Copy):
		return m.copyDetailsField(held, cursor)
	}

	return m, nil
}

func (m model) setDetailsCursor(held details, idx int) model {
	m.details.cursor = clampCursor(idx, len(held.fields))
	m.details.row = held.id

	return m
}

func (m model) quitKeys() []string {
	switch m.screen {
	case screenSchedules:
		return m.keys.Schedules.Quit
	case screenArchive:
		return m.keys.Archive.Quit
	default:
		return m.keys.Agents.Quit
	}
}

func (m model) copyDetailsField(held details, idx int) (tea.Model, tea.Cmd) {
	m.details.flashed = idx + 1

	return m, tea.Batch(m.copyCmd(held.fields[idx].value), flashRevertTick())
}

func (m model) detailsLines() int {
	if m.height <= 0 {
		return 0
	}

	lines := int(m.detailsHeight)
	if m.detailsHeight < 1 {
		lines = int(float64(m.height) * m.detailsHeight)
	}

	return min(lines, max(m.height/2-detailsChromeLines, 0))
}

const detailsChromeLines = 4

func (m model) detailsView() string {
	held := m.hoveredDetails()
	if !m.detailsDrawn(held) {
		return ""
	}

	lines := m.detailsLines()
	cursor := m.detailsCursor(held)
	rows := m.detailFieldRows(held.fields, cursor)

	return m.detailsRule() +
		m.detailsTitle(held) +
		renderList(rows, cursor, lines) +
		m.detailsRule()
}

func (m model) detailsRule() string {
	if m.width <= 0 {
		return "\n"
	}

	return dimStyle.Render(strings.Repeat("─", m.width)) + "\n"
}

func (m model) detailsTitle(held details) string {
	title := held.title
	if held.kind != "" {
		title = held.kind + ": " + title
	}

	return titleStyleFor(m.details.focused).Render(clip(title, m.lineWidth())) + "\n\n"
}

func (m model) detailFieldRows(fields []recordField, cursor int) []string {
	labels := labelColumn(fields)
	rows := make([]string, 0, len(fields))

	const gutter = 2

	for i, field := range fields {
		prefix := "  "
		if i == cursor {
			prefix = cursorStyleFor(m.details.focused).Render("› ")
		}

		label := dimStyle.Render(padRight(field.label, labels))
		value := clip(field.value, max(m.lineWidth()-labels-gutter-1, 1))
		if m.details.flashed == i+1 {
			value = flashFieldStyle.Render(value)
		}

		rows = append(rows, prefix+label+" "+value)
	}

	return rows
}

func (m model) lineWidth() int {
	if m.width <= 0 {
		return nameWidth
	}

	return m.width
}

func labelColumn(fields []recordField) int {
	widest := 0
	for _, field := range fields {
		widest = max(widest, len([]rune(field.label)))
	}

	return widest
}

func padRight(text string, width int) string {
	pad := width - len([]rune(text))
	if pad <= 0 {
		return text
	}

	return text + strings.Repeat(" ", pad)
}

var flashFieldStyle = lipgloss.NewStyle().Background(colorAccent).Foreground(lipgloss.Color("0"))
