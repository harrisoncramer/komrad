package ui

import (
	"regexp"
	"strings"

	"weave/internal/ui/paint"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type filterState struct {
	typing  bool
	query   string
	pattern *regexp.Regexp
	broken  bool
	// origin is the row the cursor sat on when the search opened, which abandoning it returns to.
	origin string
}

func (f filterState) isStanding() bool {
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

func (f filterState) highlight(palette paint.Palette, name string, base lipgloss.Style) string {
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
		writeStyled(&b, palette.Match(), name[span[0]:span[1]])
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
	m.beginFilter()

	return m, nil
}

// beginFilter starts a search from nothing, keeping the cursor on the row it was on as the rows
// the last query hid come back.
func (m *model) beginFilter() {
	origin := m.hoveredFilterKey()

	m.filter.typing = true
	m.filter.setQuery("")
	m.filter.origin = origin
	m.placeFilteredCursor(origin)
}

func (m *model) enterScreen(next screen) {
	// A pane opened on a screen that takes these keys would follow the reader onto one that does
	// not, where no key is left to close it, so it goes while the screen it belongs to is still up.
	if !next.opensInfoPane() && m.info.kind != infoNothing {
		*m = m.closeInfo()
	}

	if m.screen != next {
		m.filter = filterState{}
	}

	m.screen = next

	// Every way onto a screen runs through here, the ones nobody pressed a key for included, so
	// this is where a screen that had taken the window hands it back rather than leaving a pane
	// covered by a zoom nothing asked for.
	m.reconcileSolo()
}

// takeKey applies one keypress to a search being typed at, and reports whether the keypress was
// one of the search's.
func (f *filterState) takeKey(key tea.KeyMsg) bool {
	switch k := key.String(); {
	case k == "esc":
		f.abandon()
	case k == "enter":
		f.typing = false
	case k == "backspace":
		f.setQuery(paint.TrimLastRune(f.query))
	case key.Type == tea.KeySpace:
		f.setQuery(f.query + " ")
	case key.Type == tea.KeyRunes:
		f.setQuery(f.query + string(key.Runes))
	default:
		return false
	}

	return true
}

// abandon drops the search and the row it was going to leave you on, which makes escape the way
// back out rather than a way of keeping what you typed.
func (f *filterState) abandon() {
	f.typing = false
	f.setQuery("")
}

// clearSearch drops a search that is standing and puts the cursor back where it opened.
func (m model) clearSearch() (tea.Model, tea.Cmd) {
	origin := m.filter.origin
	m.filter.abandon()
	m.placeFilteredCursor(origin)
	m.filter.origin = ""

	return m, nil
}

func (m model) updateFilterInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, m.hide()
	}

	abandoning := key.String() == "esc"

	hovered := m.hoveredFilterKey()
	if !m.filter.takeKey(key) {
		return m, nil
	}

	if abandoning {
		m.placeFilteredCursor(m.filter.origin)
		m.filter.origin = ""

		return m, nil
	}

	// Typing walks the cursor onto what it has found so far, so the search says where it is
	// rather than only which rows it likes.
	if next, ok := m.matchAt(m.filter.origin, 0); ok {
		m.placeFilteredCursor(next)

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

func filterRows(rows []agentRow) []filterRow {
	return xslice.Map(rows, func(row agentRow) filterRow {
		name := row.item.agent.Name
		if row.header {
			name = row.workflow.Name
		}

		return filterRow{key: rowKey(row), name: name}
	})
}

func placeCursor(found, cursor, length int) int {
	if found >= 0 {
		return found
	}

	return clampCursor(cursor, length)
}

// view draws the search line whether or not there is a search.
func (f filterState) view(palette paint.Palette) string {
	return f.line(palette) + "\n\n"
}

func (f filterState) line(palette paint.Palette) string {
	query := f.query
	if f.broken {
		query = palette.Broken().Render(query)
	}

	slash := palette.Cursor()
	if !f.typing && !f.isStanding() {
		slash = dimStyle
	}

	line := slash.Render("/") + " " + query
	if f.typing {
		line += palette.Cursor().Render("▏")
	}

	return line
}

// matchAt is the row a search lands on, counted from the row given: zero is the first match at
// or after it, and a step walks the matches from there, wrapping at either end.
func (m model) matchAt(from string, delta int) (string, bool) {
	spec := m.screen.spec().filter
	if spec == nil || spec.rows == nil || m.filter.pattern == nil {
		return "", false
	}

	rows := spec.rows(m)
	if len(rows) == 0 {
		return "", false
	}

	start := 0
	for i, row := range rows {
		if row.key == from {
			start = i

			break
		}
	}

	// A step of nothing means the row the cursor is already on counts, which is what makes
	// typing land on the nearest match rather than skipping past it.
	step, walk := 0, delta
	if delta == 0 {
		walk = 1
	} else {
		step = 1
	}

	for ; step <= len(rows); step++ {
		at := ((start+walk*step)%len(rows) + len(rows)) % len(rows)
		if m.filter.doesMatch(rows[at].name) {
			return rows[at].key, true
		}
	}

	return "", false
}

// jumpMatch walks the cursor to the next row the search matches, and answers only while a search
// stands.
func (m model) jumpMatch(delta int) (tea.Model, tea.Cmd) {
	next, ok := m.matchAt(m.hoveredFilterKey(), delta)
	if !ok {
		return m, nil
	}

	m.placeFilteredCursor(next)

	return m, nil
}
