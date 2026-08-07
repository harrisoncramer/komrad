package ui

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The filter over whichever list is on screen. It is one state on the model rather than one
// per screen, since only the screen being looked at can be narrowed, and it is dropped on
// the way to another. Nothing about it reaches the service: every screen already holds its
// whole listing, so a pattern is answered from what is on screen.

// filterState is the pattern the list on screen is narrowed by.
type filterState struct {
	// typing is whether the input has the keys, which it takes whole so a letter that is
	// bound elsewhere still types.
	typing bool
	// query is what has been typed, kept once the input closes so the list stays narrowed
	// for the keys that act on it.
	query string
	// pattern is the compiled query, nil while there is nothing to match or while the query
	// is not a pattern yet, both of which draw every row.
	pattern *regexp.Regexp
	// broken says the query will not compile, which draws the input in red rather than
	// showing an empty list while a bracket is half typed.
	broken bool
}

// standing reports whether the list is narrowed by a filter the user can still act on, which
// is what makes the back key clear it rather than leave the screen.
func (f filterState) standing() bool {
	return f.query != ""
}

// setQuery installs what has been typed and compiles it. A pattern in lower case matches
// either case, since typing an upper case letter is how a case is asked for.
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

// doesMatch reports whether a row's name is drawn under the current pattern. Nothing to match
// against keeps every row.
func (f filterState) doesMatch(name string) bool {
	if f.pattern == nil {
		return true
	}

	return f.pattern.MatchString(name)
}

// filterAgentRows holds back the rows a tree does not draw under the current pattern. A
// folder whose own name matches is drawn whole, since the pattern named the folder rather
// than anything in it, and a folder holding a match keeps its header so the agents left under
// it still read as nested.
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

// highlight draws the part of a name the pattern matched in the match colour and the rest in
// the style the row would have had. The segments are rendered one at a time rather than
// nested, since a style inside another ends in a reset that would drop the outer colour for
// the rest of the row.
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

// writeStyled writes a styled segment, passing over an empty one, which lipgloss would
// otherwise write as a pair of escapes around nothing.
func writeStyled(b *strings.Builder, style lipgloss.Style, segment string) {
	if segment == "" {
		return
	}

	b.WriteString(style.Render(segment))
}

// openFilter gives the input the keys, leaving whatever is already typed to be edited.
func (m model) openFilter() (tea.Model, tea.Cmd) {
	m.filter.typing = true

	return m, nil
}

// clearFilter drops the pattern and puts every row back.
func (m model) clearFilter() (tea.Model, tea.Cmd) {
	m.filter.typing = false

	return m.typeIntoFilter("")
}

// enterScreen moves to a screen, taking any filter off when that is a different list, since a
// pattern narrows the list it was typed on rather than following the user to the next one. A
// form is an overlay rather than a screen left, so one closing back onto the list it opened
// from keeps what was typed there.
func (m *model) enterScreen(next screen) {
	if m.screen != next && !m.screen.isForm() {
		m.filter = filterState{}
	}

	m.screen = next
}

// updateFilterInput drives the input, which owns every key while it is open. enter keeps the
// pattern and hands the keys to the rows it left, and esc drops both, since the screen's back
// key opens the input again rather than clearing it: esc is the one step out of the filter
// altogether. Nothing else here acts on a row, since a key that typed and acted at once could
// not be used for either.
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

// typeIntoFilter narrows the list to a new pattern and squares the screen with it. The cursor
// follows the row it was on where the pattern still draws that row, since the row it lands on
// is what the next key acts on.
func (m model) typeIntoFilter(query string) (tea.Model, tea.Cmd) {
	hovered := m.hoveredFilterKey()
	m.filter.setQuery(query)
	m.placeFilteredCursor(hovered)

	return m, nil
}

// hoveredFilterKey identifies the row under the cursor on the screen being filtered, in
// whatever the screen's rows are keyed by, or empty when the cursor is on nothing.
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
		// No other screen draws a list the filter reaches.
	}

	return ""
}

// placeFilteredCursor puts the cursor back on the row the given key names, or as near as the
// narrowed list allows, and restages what it lands on.
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
		// No other screen draws a list the filter reaches.
	}
}

// placeCursor is the row a cursor lands on once the list has been narrowed: the row it was on
// where that row is still drawn, and the nearest one to where it was where it is not.
func placeCursor(found, cursor, length int) int {
	if found >= 0 {
		return found
	}

	return clampCursor(cursor, length)
}

// filterView is the input over a list, drawn while it is open and while a pattern it left
// behind is still narrowing the rows. A query that will not compile is drawn in red, which is
// the one thing that says why the list did not narrow.
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
