package ui

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type filterState struct {
	typing bool

	query string

	pattern *regexp.Regexp

	broken bool
}

func (f filterState) standing() bool {
	return f.query != ""
}

func (f *filterState) setQuery(query string) {
	f.query = query
	f.pattern, f.broken = nil, false

	if strings.TrimSpace(query) == "" {
		return
	}

	expr := query
	if strings.ToLower(query) == query {
		expr = "(?i)" + query
	}

	pattern, err := regexp.Compile(expr)
	if err != nil {
		f.broken = true

		return
	}

	f.pattern = pattern
}

func (f filterState) doesMatch(name string) bool {
	if f.pattern == nil {
		return true
	}

	return f.pattern.MatchString(name)
}

func (f filterState) filterAgentRows(rows []agentRow) []agentRow {
	if f.pattern == nil {
		return rows
	}

	named := make(map[string]bool, len(rows))
	holding := make(map[string]bool, len(rows))
	for _, r := range rows {
		switch {
		case r.header && f.doesMatch(r.workflow.Name):
			named[r.workflow.ID] = true
		case !r.header && f.doesMatch(r.item.agent.Name):
			holding[r.workflow.ID] = true
		}
	}

	kept := make([]agentRow, 0, len(rows))
	for _, r := range rows {
		switch {
		case r.header:
			if named[r.workflow.ID] || holding[r.workflow.ID] {
				kept = append(kept, r)
			}
		case named[r.workflow.ID] || f.doesMatch(r.item.agent.Name):
			kept = append(kept, r)
		}
	}

	return kept
}

func (f filterState) highlight(name string, base lipgloss.Style) string {
	if f.pattern == nil {
		return base.Render(name)
	}

	spans := f.pattern.FindAllStringIndex(name, -1)
	if len(spans) == 0 {
		return base.Render(name)
	}

	var b strings.Builder
	at := 0
	for _, span := range spans {
		writeStyled(&b, base, name[at:span[0]])
		writeStyled(&b, matchStyle, name[span[0]:span[1]])
		at = span[1]
	}
	writeStyled(&b, base, name[at:])

	return b.String()
}

func writeStyled(b *strings.Builder, style lipgloss.Style, segment string) {
	if segment == "" {
		return
	}

	b.WriteString(style.Render(segment))
}

func (m model) openFilter() (tea.Model, tea.Cmd) {
	m.filter.typing = true

	return m, nil
}

func (m model) clearFilter() (tea.Model, tea.Cmd) {
	m.filter.typing = false

	return m.typeIntoFilter("")
}

func (m *model) enterScreen(next screen) {
	if m.screen != next && !m.screen.isForm() {
		m.filter = filterState{}
	}

	m.screen = next
}

func (m model) updateFilterInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k := key.String(); {
	case k == "esc":
		return m.clearFilter()
	case k == "enter":
		m.filter.typing = false

		return m, nil
	case k == "ctrl+c":
		return m, m.hide()
	case k == "backspace":
		return m.typeIntoFilter(trimLastRune(m.filter.query))
	case key.Type == tea.KeySpace:
		return m.typeIntoFilter(m.filter.query + " ")
	case key.Type == tea.KeyRunes:
		return m.typeIntoFilter(m.filter.query + string(key.Runes))
	}

	return m, nil
}

func (m model) typeIntoFilter(query string) (tea.Model, tea.Cmd) {
	hovered := m.hoveredFilterKey()
	m.filter.setQuery(query)
	m.placeFilteredCursor(hovered)

	return m, nil
}

func (m model) hoveredFilterKey() string {
	switch m.screen {
	case screenAgents:
		return rowKeyAt(m.agentRows(), m.tree.cursor)
	case screenArchive:
		return rowKeyAt(m.archiveRows(), m.archive.cursor)
	case screenSchedules:
		if item, ok := m.selectedSchedule(); ok {
			return item.schedule.ID
		}
	case screenActions:
		if action, ok := m.selectedAction(); ok {
			return action.name
		}
	default:
	}

	return ""
}

func (m *model) placeFilteredCursor(hovered string) {
	switch m.screen {
	case screenAgents:
		rows := m.agentRows()
		m.tree.cursor = placeCursor(rowIndexOfKey(rows, hovered), m.tree.cursor, len(rows))
		m.stageSelection()
	case screenArchive:
		rows := m.archiveRows()
		m.archive.cursor = placeCursor(rowIndexOfKey(rows, hovered), m.archive.cursor, len(rows))
		m.stageArchive()
	case screenSchedules:
		rows := m.scheduleRows()
		found := -1
		for i, item := range rows {
			if item.schedule.ID == hovered {
				found = i
			}
		}
		m.schedules.cursor = placeCursor(found, m.schedules.cursor, len(rows))
		m.stageSchedule()
	case screenActions:
		rows := m.actionRows()
		found := -1
		for i, action := range rows {
			if action.name == hovered {
				found = i
			}
		}
		m.actions.cursor = placeCursor(found, m.actions.cursor, len(rows))
		m.stageAction()
	default:
	}
}

func placeCursor(found, cursor, length int) int {
	if found >= 0 {
		return found
	}

	return clampCursor(cursor, length)
}

func (m model) filterView() string {
	if !m.filter.typing && !m.filter.standing() {
		return ""
	}

	query := m.filter.query
	if m.filter.broken {
		query = brokenStyle.Render(query)
	}

	line := cursorStyle.Render("/") + " " + query
	if m.filter.typing {
		line += cursorStyle.Render("▏")
	}

	return line + "\n\n"
}
