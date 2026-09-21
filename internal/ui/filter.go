package ui

import (
	"regexp"
	"strings"

	"weave/internal/ui/paint"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// filterOpenKey opens the filter on every screen that draws one.
const filterOpenKey = "/"

// filterMode is what a pattern does to the rows it does not match.
type filterMode int

const (
	filterModeSearch filterMode = iota
	filterModeFilter
)

func (m filterMode) flipped() filterMode {
	if m == filterModeFilter {
		return filterModeSearch
	}

	return filterModeFilter
}

// glyph is the character the prompt opens with.
func (m filterMode) glyph() string {
	if m == filterModeFilter {
		return "▽"
	}

	return "/"
}

func (m filterMode) String() string {
	if m == filterModeFilter {
		return "filter"
	}

	return "search"
}

type filterState struct {
	typing  bool
	query   string
	pattern *regexp.Regexp
	broken  bool
	mode    filterMode
	// over is the section the search was opened over.
	over sidebarSection
	// origin is the row the cursor sat on when the search opened, which abandoning it returns to.
	origin string
}

func (f filterState) isStanding() bool {
	return f.query != ""
}

// isNarrowing reports whether the pattern is taking rows off the screen.
func (f filterState) isNarrowing() bool {
	return f.mode == filterModeFilter && f.pattern != nil
}

// doesNarrow reports whether the pattern is taking rows off one section of the sidebar.
func (f filterState) doesNarrow(section sidebarSection) bool {
	return f.isNarrowing() && f.over == section
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

	return palette.Highlight(name, matchesIn(f.pattern, name), base)
}

// filterTarget is the search and the cursor a key drives, and rows is nil on a screen that
// draws no filter.
type filterTarget struct {
	state *filterState
	rows  filtered
}

func (m *model) filterTarget() filterTarget {
	return filterTarget{state: &m.filter, rows: m.filteredRows()}
}

// filteredRows is the cursor the search drives, the record's while the record holds the keys.
func (m model) filteredRows() filtered {
	if m.isDetailsFocused() {
		return detailsFilter{}
	}

	rows, _ := m.active().(filtered)

	return rows
}

func (m model) openFilter() (tea.Model, tea.Cmd) {
	m.beginFilter()

	return m, nil
}

// beginFilter starts a search from nothing, keeping the cursor on the row it was on.
func (m *model) beginFilter() {
	origin := m.hoveredFilterKey()

	filter := m.filterTarget().state
	filter.typing = true
	filter.mode = filterModeSearch
	filter.over = m.keyholder()
	filter.setQuery("")
	filter.origin = origin
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

// takeKey applies one keypress to a search being typed at, and reports whether it was one of
// the search's.
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

// abandon drops the search and the row it was going to leave you on.
func (f *filterState) abandon() {
	f.typing = false
	f.mode = filterModeSearch
	f.setQuery("")
}

// clearSearch drops a standing search and puts the cursor back where the search opened, unless
// the keys have since moved to the other section.
func (m model) clearSearch() (tea.Model, tea.Cmd) {
	filter := m.filterTarget().state
	origin, over := filter.origin, filter.over
	filter.abandon()
	filter.origin = ""
	if over == m.keyholder() {
		m.placeFilteredCursor(origin)
	}

	return m, nil
}

func (m model) updateFilterInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, m.hide()
	}

	if isBound(key.String(), m.keys.Details.Focus) && m.isDetailsScreen() {
		return m.closeSearchAndMoveKeys()
	}

	abandoning := key.String() == "esc"

	hovered := m.hoveredFilterKey()
	filter := m.filterTarget().state
	switch {
	case key.String() == "tab":
		filter.mode = filter.mode.flipped()
	case !filter.takeKey(key):
		return m, nil
	}

	if abandoning {
		m.placeFilteredCursor(filter.origin)
		filter.origin = ""

		return m, nil
	}

	// Typing walks the cursor onto what it has found so far, so the search says where it is
	// rather than only which rows it likes.
	if next, ok := m.matchAt(filter.origin, 0); ok {
		m.placeFilteredCursor(next)

		return m, nil
	}

	m.placeFilteredCursor(hovered)

	return m, nil
}

// closeSearchAndMoveKeys answers the focus key while the search is being typed: the input
// closes with the pattern standing, the keys move, and a record they land on opens on its
// nearest match.
func (m model) closeSearchAndMoveKeys() (tea.Model, tea.Cmd) {
	if !m.isDetailsDrawn(m.hoveredDetails()) {
		return m, nil
	}

	m.filter.typing = false

	next, cmd := m.toggleDetailsFocus()
	if !next.isDetailsFocused() {
		return next, cmd
	}

	if found, ok := next.matchAt(next.hoveredFilterKey(), 0); ok {
		next.placeFilteredCursor(found)
	}

	return next, cmd
}

func (m model) hoveredFilterKey() string {
	rows := m.filterTarget().rows
	if rows == nil {
		return ""
	}

	return rows.hoveredKey(m)
}

func (m *model) placeFilteredCursor(hovered string) {
	if rows := m.filterTarget().rows; rows != nil {
		rows.placeAt(m, hovered)
	}
}

// agentRowName is the text a search reads a row of the tree by: the workflow's own name on a
// header, the agent's on a row.
func agentRowName(row agentRow) string {
	if row.header {
		return row.workflow.Name
	}

	return row.item.agent.Name
}

// agentRowDepth is how far a row of the tree is indented: a header, an agent under it, or a
// child under that.
func agentRowDepth(row agentRow) int {
	switch {
	case row.header:
		return 0
	case row.child:
		return 2
	}

	return 1
}

type narrowedParams[T any] struct {
	rows []T
	// depth is how far a row is indented.
	depth func(T) int
	name  func(T) string
}

// narrowed is the rows a filter leaves on screen: each match, the rows above it and the rows
// under it.
func narrowed[T any](filter filterState, params narrowedParams[T]) []T {
	if !filter.doesNarrow(sectionList) {
		return params.rows
	}

	rows := params.rows
	matched := make([]bool, len(rows))
	for i, row := range rows {
		matched[i] = filter.doesMatch(params.name(row))
	}

	keep := make([]bool, len(rows))

	// The stack holds the rows the one being read sits under, and lit counts how many of those
	// the pattern matched.
	var stack []int
	lit := 0
	for i, row := range rows {
		depth := params.depth(row)
		for len(stack) > 0 && params.depth(rows[stack[len(stack)-1]]) >= depth {
			if matched[stack[len(stack)-1]] {
				lit--
			}
			stack = stack[:len(stack)-1]
		}

		keep[i] = matched[i] || lit > 0
		if matched[i] {
			for _, above := range stack {
				keep[above] = true
			}
			lit++
		}

		stack = append(stack, i)
	}

	kept := make([]T, 0, len(rows))
	for i, row := range rows {
		if keep[i] {
			kept = append(kept, row)
		}
	}

	return kept
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

	line := slash.Render(f.mode.glyph()) + " " + query
	if f.typing {
		line += palette.Cursor().Render("▏")
	}

	return line
}

// matchAt is the row a search lands on, counted from the row given and wrapping at either end.
func (m model) matchAt(from string, delta int) (string, bool) {
	target := m.filterTarget()
	filter := target.state
	if target.rows == nil || filter.pattern == nil {
		return "", false
	}

	rows := target.rows.filterRows(m)
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
		if filter.doesMatch(rows[at].name) {
			return rows[at].key, true
		}
	}

	return "", false
}

func (m model) jumpFilterMatch(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Filter.PrevMatch) {
		return m.jumpMatch(-1)
	}

	return m.jumpMatch(1)
}

// sidebarRow is one row a search can land on, in the section that draws it.
type sidebarRow struct {
	section sidebarSection
	row     filterRow
}

// sidebarRows is every row a search can land on, the list's and then the hovered record's.
func (m model) sidebarRows() []sidebarRow {
	var rows []sidebarRow
	if list, ok := m.active().(filtered); ok {
		rows = sectionRows(sectionList, list.filterRows(m))
	}
	if m.isDetailsDrawn(m.hoveredDetails()) {
		rows = append(rows, sectionRows(sectionRecord, (detailsFilter{}).filterRows(m))...)
	}

	return rows
}

func sectionRows(section sidebarSection, rows []filterRow) []sidebarRow {
	return xslice.Map(rows, func(row filterRow) sidebarRow {
		return sidebarRow{section: section, row: row}
	})
}

// jumpMatch walks the cursor to the next row the search matches, on through the record and
// round again, moving the keys with it.
func (m model) jumpMatch(delta int) (tea.Model, tea.Cmd) {
	rows := m.sidebarRows()
	if m.filter.pattern == nil || len(rows) == 0 {
		return m, nil
	}

	from := sidebarRow{section: m.keyholder(), row: filterRow{key: m.hoveredFilterKey()}}
	start := 0
	for i, row := range rows {
		if row.section == from.section && row.row.key == from.row.key {
			start = i

			break
		}
	}

	for step := 1; step <= len(rows); step++ {
		at := ((start+delta*step)%len(rows) + len(rows)) % len(rows)
		if m.filter.doesMatch(rows[at].row.name) {
			return m.landOnMatch(rows[at])
		}
	}

	return m, nil
}

// landOnMatch puts the keys in the section a match is in and the cursor on it.
func (m model) landOnMatch(target sidebarRow) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case target.section == sectionRecord && !m.isDetailsFocused():
		m, cmd = m.focusDetails()
		if !m.isDetailsFocused() {
			return m, nil
		}
	case target.section == sectionList && m.isDetailsFocused():
		m = m.blurDetails()
	}

	m.placeFilteredCursor(target.row.key)

	return m, cmd
}
