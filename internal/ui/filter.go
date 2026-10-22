package ui

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/searchsyntax"
	"komrad/internal/ui/field"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
	"komrad/internal/vim"

	tea "charm.land/bubbletea/v2"
)

// filterOpenKey opens the filter on every screen that renders one.
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
	input   field.Line
	matcher *regexp.Regexp
	broken  bool
	mode    filterMode
	// openedIn is the section the keys were in when the search opened.
	openedIn list.Section
	// origin is the row the cursor sat on when the search opened, which abandoning it returns to.
	origin string
	// appliedAt is the row the cursor sat on when the search was last applied.
	appliedAt     string
	debounce      time.Duration
	minLength     int
	compiledQuery string
	problem       string
	qualifiers    qualifierState
}

// qualifierState is the qualifiers a search reads and what the server found for its says: one.
type qualifierState struct {
	grammar *list.Grammar
	// clock is what the grammar reads relative dates against.
	clock      screen.Clock
	clauses    []searchsyntax.Clause
	compiledAt time.Time
	said       saidState
}

// fail marks the search broken for the reason given.
func (f *filterState) fail(reason string) {
	f.broken, f.problem = true, reason
}

type filterOption func(*filterState)

func withDebounce(wait time.Duration) filterOption {
	return func(f *filterState) {
		f.debounce = wait
	}
}

func withGrammar(grammar list.Grammar, clock screen.Clock) filterOption {
	return func(f *filterState) {
		f.qualifiers.grammar, f.qualifiers.clock = &grammar, clock
	}
}

func withMinLength(length int) filterOption {
	return func(f *filterState) {
		f.minLength = length
	}
}

func (f filterState) query() string {
	return f.input.Value()
}

func (f filterState) isStanding() bool {
	return f.query() != ""
}

func (f filterState) pattern() list.Pattern {
	return list.Pattern{
		Typed:      f.query(),
		Query:      f.compiledQuery,
		Regexp:     f.matcher,
		Broken:     f.broken,
		Clauses:    f.qualifiers.clauses,
		CompiledAt: f.qualifiers.compiledAt,
		Filtering:  f.mode == filterModeFilter && !f.typing,
		OpenedIn:   f.openedIn,
		Said: list.Said{
			Text:    f.qualifiers.said.text,
			Hits:    f.qualifiers.said.hits,
			Waiting: f.qualifiers.said.waiting,
		},
	}
}

// openDesc names the key that opens the search by the pattern standing.
func (f filterState) openDesc() string {
	if f.isStanding() {
		return f.mode.String() + ": " + f.query()
	}

	return "search"
}

func (f filterState) clearDesc() string {
	return "clear " + f.mode.String()
}

func (f *filterState) setQuery(query string) {
	f.input.Set(query)
	f.compile()
}

func (f *filterState) open(opts ...filterOption) {
	f.typing = true
	query := f.query()
	if query == "" {
		f.mode = filterModeSearch
	}
	f.debounce, f.minLength, f.qualifiers.grammar = 0, 0, nil
	for _, opt := range opts {
		opt(f)
	}
	f.setQuery(query)
}

func (f filterState) isTooShort() bool {
	query := f.query()

	return strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) < f.minLength
}

func (f filterState) isSearched() bool {
	return f.pattern().HasQuery() || f.broken
}

func (f filterState) isSettled() bool {
	return f.compiledQuery == f.query()
}

func (f filterState) settleCmd(clock screen.Clock, settled func(query string) tea.Msg) tea.Cmd {
	query := f.query()

	return clock.After(f.debounce, func(time.Time) tea.Msg {
		return settled(query)
	})
}

func (f *filterState) settle(query string) bool {
	if query != f.query() || f.isSettled() {
		return false
	}

	f.compile()

	return true
}

// compile reads the pattern out of what the user typed.
func (f *filterState) compile() {
	query := f.query()
	f.matcher, f.broken, f.compiledQuery = nil, false, query
	f.problem, f.qualifiers.clauses = "", nil
	defer f.askSaid()

	if f.isTooShort() {
		return
	}

	expr := query
	if f.qualifiers.grammar != nil {
		f.qualifiers.compiledAt = f.qualifiers.clock.Now()
		parsed, err := f.qualifiers.grammar.Parse(query, f.qualifiers.compiledAt)
		if err != nil {
			f.fail(errui.GetReason(err))

			return
		}

		f.qualifiers.clauses, expr = parsed.Clauses, parsed.Text()
		if expr == "" {
			return
		}
	}

	if strings.ToLower(expr) == expr {
		expr = "(?i)" + expr
	}

	pattern, err := regexp.Compile(expr)
	if err != nil {
		f.broken = true

		return
	}

	f.matcher = pattern
}

// filteredRows is every row the search can land on in the pane holding the keys, and false on a
// screen that renders no filter.
func (m *model) filteredRows() ([]list.Row, bool) {
	l, ok := m.filteredList()
	if !ok {
		return nil, false
	}

	return l.filterRows(m, m.inputs()), true
}

func (m *model) filteredList() (keyedList, bool) {
	if m.isDetailsFocused(m.inputs()) {
		return m.detailsList(), true
	}

	lister, ok := m.active().(activeLister)
	if !ok {
		return nil, false
	}

	return lister.activeList(), true
}

// beginFilter starts a search from nothing, keeping the cursor on the row it was on.
func (m *model) beginFilter() {
	origin := m.hoveredFilterKey()

	var opts []filterOption
	if screen, ok := m.active().(queryGrammarer); ok && !m.isDetailsFocused(m.inputs()) {
		if grammar, reads := screen.queryGrammar(); reads {
			opts = append(opts, withGrammar(grammar, m.clock))
		}
	}

	filter := &m.filter
	filter.open(opts...)
	filter.openedIn = m.keyholder(m.inputs())
	filter.origin = origin
	m.placeFilteredCursor(origin)
}

func (m *model) enterScreen(next screen.ID) tea.Cmd {
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
	if b, ok := m.active().(columnFocuser); ok {
		b.blurBlock()
	}
	m.reading.focused = false

	if l, ok := m.active().(screenLeaver); ok {
		return m.apply(l.leaveScreen(m))
	}

	return nil
}

// takeKey applies one keypress to a search being typed at, with vim reading it first when
// register is set, and reports whether it was one of the search's.
func (f *filterState) takeKey(key tea.KeyPressMsg, register *vim.Register) bool {
	switch f.input.Take(key, register) {
	case field.LineLeft:
		f.abandon()
	case field.LineEntered:
		f.typing = false
		f.compile()
	case field.LineChanged:
		if f.debounce == 0 || f.isTooShort() {
			f.compile()
		}
	case field.LineIgnored:
		return false
	case field.LineKept:
	}

	return true
}

// abandon drops the search and the row it was going to leave you on.
func (f *filterState) abandon() {
	f.typing = false
	f.mode = filterModeSearch
	f.setQuery("")
}

// clearFilter drops a standing search and puts the cursor back where the search opened, unless
// the keys have since moved to the other section.
func (m *model) clearFilter() {
	filter := &m.filter
	origin, openedIn := filter.origin, filter.openedIn
	filter.abandon()
	filter.origin = ""
	if openedIn == m.keyholder(m.inputs()) {
		m.placeFilteredCursor(origin)
	}
	m.reconcileSolo()
}

func (m *model) updateFilterInput(key tea.KeyPressMsg) tea.Cmd {
	if key.String() == "ctrl+c" {
		return m.hide()
	}

	// Typing lights what it has found and moves nothing; enter is what applies the search.
	filter := &m.filter
	switch key.String() {
	case "tab":
		filter.mode = filter.mode.flipped()
	case "enter":
		m.applyFilter()

		return tea.Batch(m.waitToStage(), m.saysCmd())
	default:
		filter.takeKey(key, m.register)
		m.reconcileSolo()
	}

	return m.saysCmd()
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
	if m.filter.pattern().IsNarrowing() && m.isFilterRowShown(hovered) {
		found = hovered
	}

	m.placeFilteredCursor(found)
	m.filter.appliedAt = m.hoveredFilterKey()
}

// isFilterRowShown reports whether the pane holding the keys still renders a row.
func (m *model) isFilterRowShown(key string) bool {
	rows, _ := m.filteredRows()

	return slices.ContainsFunc(rows, func(row list.Row) bool { return row.Key == key })
}

// jumpIntoDetails gives the record the keys and lands them on its nearest match when a search is
// up.
func (m *model) jumpIntoDetails() tea.Cmd {
	cmd := m.focusDetails()
	if !m.isDetailsFocused(m.inputs()) {
		return cmd
	}

	if found, ok := m.matchAt(0); ok {
		m.placeFilteredCursor(found)
	}

	return cmd
}

func (m *model) hoveredFilterKey() string {
	l, ok := m.filteredList()
	if !ok {
		return ""
	}

	return l.hovered(m, m.inputs())
}

func (m *model) placeFilteredCursor(hovered string) {
	if l, ok := m.filteredList(); ok {
		m.apply(l.place(m, m.inputs(), hovered))
	}
}

// agentRowName is the text a search reads a row of the tree by: the workflow's own name on a
// header, the slug on a task, the agent's on a row.
func agentRowName(row agentRow) string {
	if row.isTask() {
		return row.task.Slug
	}
	if row.header {
		return row.workflow.Name
	}

	return row.item.Agent.Name
}

// agentRowDepth is how far a row of the tree is indented: a header, an agent or a task under it,
// or a reviewer under its task.
func agentRowDepth(row agentRow) int {
	switch {
	case row.isTask():
		return 1 + row.depth
	case row.header:
		return 0
	case row.underTask:
		return 2 + row.depth
	}

	return 1
}

// stepFilterMatch walks the cursor delta rows the search matches along.
func (m *model) stepFilterMatch(delta int) tea.Cmd {
	if found, ok := m.matchAt(delta); ok {
		m.placeFilteredCursor(found)
	}

	return m.waitToStage()
}

// matchAt is the row the search lands on in the pane holding the keys, counted from the hovered
// row and wrapping at either end, the hovered row itself counting for a step of nothing.
func (m *model) matchAt(delta int) (string, bool) {
	rows, ok := m.filteredRows()
	pattern := m.filter.pattern()
	if !ok || !pattern.HasQuery() || len(rows) == 0 {
		return "", false
	}

	start := 0
	hovered := m.hoveredFilterKey()
	for i, row := range rows {
		if row.Key == hovered {
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
		if pattern.Admits(rows[at].Name, rows[at].Fits) {
			return rows[at].Key, true
		}
	}

	return "", false
}

// filterDesc names the filter key by the search standing in the list.
func (m *model) filterDesc() string {
	return filterDescOf(m)
}

func (m *model) filterClearDesc() string {
	return filterClearDescOf(m)
}

func (m *model) clickSearch(key string) tea.Cmd {
	to, err := strconv.Atoi(key)
	if err != nil {
		return nil
	}

	if m.isViewsOpen() {
		m.closeViews()
	}

	entered := m.enterStop(stop(to))
	if m.isTyping() {
		return entered
	}

	return tea.Batch(entered, m.route(keys.Press(filterOpenKey)))
}

// filterBy narrows the list up as if the user typed and applied the query, in the grammar the
// screen up reads.
func (m *model) filterBy(query string) {
	var opts []filterOption
	if s, ok := m.active().(queryGrammarer); ok {
		if grammar, reads := s.queryGrammar(); reads {
			opts = append(opts, withGrammar(grammar, m.clock))
		}
	}

	m.filter.open(opts...)
	m.filter.mode = filterModeFilter
	m.filter.openedIn = list.SectionList
	m.filter.setQuery(query)
	m.applyFilter()
}
