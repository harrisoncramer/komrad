package ui

import (
	"regexp"
	"slices"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
func (m filterMode) glyph(glyphs glyph.Set) string {
	if m == filterModeFilter {
		return glyphs.Spell(glyph.Filter)
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
	// openedIn is the section the keys were in when the search opened.
	openedIn sidebarSection
	// origin is the row the cursor sat on when the search opened, which abandoning it returns to.
	origin string
}

func (f filterState) isStanding() bool {
	return f.query != ""
}

// isNarrowing reports whether the pattern is taking rows off the screen.
func (f filterState) isNarrowing() bool {
	return f.mode == filterModeFilter && f.pattern != nil && !f.typing
}

// doesNarrow reports whether the pattern is taking rows off one pane, which it does only in the
// pane it was typed in.
func (f filterState) doesNarrow(section sidebarSection) bool {
	return f.isNarrowing() && f.openedIn == section
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

// filteredRows is every row the search can land on in the pane holding the keys, and false on a
// screen that draws no filter.
func (m model) filteredRows() ([]filterRow, bool) {
	if m.isDetailsFocused() {
		return m.detailsFilterRows(), true
	}

	rows, ok := m.active().(filtered)
	if !ok {
		return nil, false
	}

	return rows.filterRows(m.shared()), true
}

func (m model) openFilter() (tea.Model, tea.Cmd) {
	m.beginFilter()

	return m, nil
}

// beginFilter starts a search from nothing, keeping the cursor on the row it was on.
func (m *model) beginFilter() {
	origin := m.hoveredFilterKey()

	filter := &m.filter
	filter.typing = true
	filter.mode = filterModeSearch
	filter.openedIn = m.keyholder()
	filter.setQuery("")
	filter.origin = origin
	m.placeFilteredCursor(origin)
}

func (m *model) enterScreen(next screen) tea.Cmd {
	// A pane opened on a screen that takes these keys would follow the reader onto one that does
	// not, where no key is left to close it, so it goes while the screen it belongs to is still up.
	if !m.isViewsOpen() {
		m.closeTerminalLeaving(next)
	}

	var left tea.Cmd
	if m.screen != next {
		left = m.leaveScreen()
		m.filter = filterState{}
	}

	m.screen = next

	// Every way onto a screen runs through here, the ones nobody pressed a key for included, so
	// this is where a screen that had taken the window hands it back rather than leaving a pane
	// covered by a zoom nothing asked for.
	m.reconcileSolo()

	return left
}

// leaveScreen hands the keys back from the block of the screen going down and puts down what it had open.
func (m *model) leaveScreen() tea.Cmd {
	if b, ok := m.active().(blocked); ok {
		b.blurBlock()
	}
	m.reading.focused = false

	if l, ok := m.active().(leaving); ok {
		return l.leave(m)
	}

	return nil
}

// takeKey applies one keypress to a search being typed at, and reports whether it was one of
// the search's.
func (f *filterState) takeKey(key tea.KeyPressMsg) bool {
	switch k := key.String(); {
	case k == "esc":
		f.abandon()
	case k == "enter":
		f.typing = false
	case k == "backspace":
		f.setQuery(paint.TrimLastRune(f.query))
	case typedText(key) != "":
		f.setQuery(f.query + typedText(key))
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

func (m model) clearSearch() (tea.Model, tea.Cmd) {
	m.clearFilter()

	return m, nil
}

// clearFilter drops a standing search and puts the cursor back where the search opened, unless
// the keys have since moved to the other section.
func (m *model) clearFilter() {
	filter := &m.filter
	origin, openedIn := filter.origin, filter.openedIn
	filter.abandon()
	filter.origin = ""
	if openedIn == m.keyholder() {
		m.placeFilteredCursor(origin)
	}
}

func (m model) updateFilterInput(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, m.hide()
	}

	// Typing lights what it has found and moves nothing; enter is what applies the search.
	filter := &m.filter
	switch {
	case key.String() == "tab":
		filter.mode = filter.mode.flipped()
	case key.String() == "enter":
		m.applyFilter()

		return m, m.waitToStage()
	case !filter.takeKey(key):
		return m, nil
	}

	return m, nil
}

// applyFilter closes the input and settles the cursor of the pane holding the keys: a search
// walks it onto the nearest match, and a filter leaves it on its row unless narrowing took that
// row off the screen.
func (m *model) applyFilter() {
	hovered := m.hoveredFilterKey()
	found, ok := m.matchAt(0)
	m.filter.typing = false
	if !ok {
		found = ""
	}
	if m.filter.isNarrowing() && m.isFilterRowShown(hovered) {
		found = hovered
	}

	m.placeFilteredCursor(found)
}

// isFilterRowShown reports whether the pane holding the keys still draws a row.
func (m model) isFilterRowShown(key string) bool {
	rows, _ := m.filteredRows()

	return slices.ContainsFunc(rows, func(row filterRow) bool { return row.key == key })
}

// jumpIntoDetails gives the record the keys and lands them on its nearest match when a search is
// up.
func (m model) jumpIntoDetails() (tea.Model, tea.Cmd) {
	next, cmd := m.focusDetails()
	if !next.isDetailsFocused() {
		return next, cmd
	}

	if found, ok := next.matchAt(0); ok {
		next.placeFilteredCursor(found)
	}

	return next, cmd
}

func (m model) hoveredFilterKey() string {
	if m.isDetailsFocused() {
		return m.hoveredDetailsKey()
	}

	rows, ok := m.active().(filtered)
	if !ok {
		return ""
	}

	return rows.hoveredKey(m.shared())
}

func (m *model) placeFilteredCursor(hovered string) {
	if m.isDetailsFocused() {
		m.placeDetailsAt(hovered)

		return
	}

	if rows, ok := m.active().(filtered); ok {
		rows.placeAt(m, m.shared(), hovered)
	}
}

// agentRowName is the text a search reads a row of the tree by: the workflow's own name on a
// header, the agent's on a row.
func agentRowName(row agentRow) string {
	if row.header {
		return row.workflow.Name
	}

	return row.item.Agent.Name
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

// view draws the search line whether or not there is a search.
func (f filterState) view(palette paint.Palette, glyphs glyph.Set) string {
	return f.line(palette, glyphs) + "\n\n"
}

func (f filterState) line(palette paint.Palette, glyphs glyph.Set) string {
	query := f.query
	if f.broken {
		query = palette.Broken().Render(query)
	}

	slash := palette.Cursor()
	switch {
	case !f.typing && !f.isStanding():
		slash = dimStyle()
	case !f.typing:
		slash = palette.Blinking(false).Cursor()
	}

	line := slash.Render(f.mode.glyph(glyphs)) + " " + query
	if f.typing {
		line += palette.Cursor().Render(glyph.TextCursor)
	}

	return line
}

// head is the rows over a block the filter searches: the search, what the last key said, and a
// blank row.
func (f filterState) head(count, said string, width int, glyphs glyph.Set) []string {
	indent := strings.Repeat(" ", docMargin())
	head := []string{fitRow(indent+f.line(paint.Panel, glyphs)+count, width)}
	if said != "" {
		head = append(head, fitRow(indent+dimStyle().Render(said), width))
	}

	return append(head, "")
}

func (m model) jumpFilterMatch(pressed string) (tea.Model, tea.Cmd) {
	return m, m.jumpToFilterMatch(pressed)
}

// jumpToFilterMatch walks the cursor to the next row the search matches, the way the key says.
func (m *model) jumpToFilterMatch(pressed string) tea.Cmd {
	delta := 1
	if isBound(pressed, m.keys.Filter.PrevMatch) {
		delta = -1
	}

	if found, ok := m.matchAt(delta); ok {
		m.placeFilteredCursor(found)
	}

	return m.waitToStage()
}

// matchAt is the row the search lands on in the pane holding the keys, counted from the hovered
// row and wrapping at either end, the hovered row itself counting for a step of nothing.
func (m model) matchAt(delta int) (string, bool) {
	rows, ok := m.filteredRows()
	if !ok || m.filter.pattern == nil || len(rows) == 0 {
		return "", false
	}

	start := 0
	hovered := m.hoveredFilterKey()
	for i, row := range rows {
		if row.key == hovered {
			start = i

			break
		}
	}

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

// filterDesc names the filter key by the search standing in the list.
func (m model) filterDesc() string {
	return filterDescOf(m.shared())
}

func (m model) filterClearDesc() string {
	return filterClearDescOf(m.shared())
}
