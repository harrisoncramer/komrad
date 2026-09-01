package ui

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type filterState struct {
	typing  bool
	query   string
	pattern *regexp.Regexp
	broken  bool
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

func (m *model) enterScreen(next screen) {
	if m.screen != next {
		m.filter = filterState{}
	}

	m.screen = next
}

// takeKey applies one keypress to a filter being typed at, and reports whether the keypress was
// one of the filter's.
func (f *filterState) takeKey(key tea.KeyMsg) bool {
	switch k := key.String(); {
	case k == "esc":
		f.typing = false
		f.setQuery("")
	case k == "enter":
		f.typing = false
	case k == "backspace":
		f.setQuery(trimLastRune(f.query))
	case key.Type == tea.KeySpace:
		f.setQuery(f.query + " ")
	case key.Type == tea.KeyRunes:
		f.setQuery(f.query + string(key.Runes))
	default:
		return false
	}

	return true
}

func (m model) updateFilterInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, m.hide()
	}

	hovered := m.hoveredFilterKey()
	if !m.filter.takeKey(key) {
		return m, nil
	}

	m.placeFilteredCursor(hovered)

	return m, nil
}

func (m model) hoveredFilterKey() string {
	spec := m.screen.spec().filter
	if spec == nil {
		return ""
	}

	return spec.hovered(m)
}

func (m *model) placeFilteredCursor(hovered string) {
	if spec := m.screen.spec().filter; spec != nil {
		spec.place(m, hovered)
	}
}

func placeCursor(found, cursor, length int) int {
	if found >= 0 {
		return found
	}

	return clampCursor(cursor, length)
}

func (m model) filterView() string {
	return m.filter.view()
}

func (f filterState) view() string {
	if !f.typing && !f.standing() {
		return ""
	}

	query := f.query
	if f.broken {
		query = brokenStyle.Render(query)
	}

	line := cursorStyle.Render("/") + " " + query
	if f.typing {
		line += cursorStyle.Render("▏")
	}

	return line + "\n\n"
}
