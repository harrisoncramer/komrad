package ui

import (
	"slices"

	"komrad/internal/ui/filter"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// filteredRows is every row the search can land on in the pane holding the keys, and false on a
// screen that renders no filter.
func (m *model) filteredRows() ([]list.Row, bool) {
	l, ok := m.filteredList()
	if !ok {
		return nil, false
	}

	return l.FilterRows(m.inputs()), true
}

func (m *model) filteredList() (keyedList, bool) {
	if m.isDetailsFocused(m.inputs()) {
		return m.detailsList(), true
	}

	lister, ok := m.active().(activeLister)
	if !ok {
		return nil, false
	}

	return lister.ActiveList(), true
}

// beginFilter starts a search from nothing, keeping the cursor on the row it was on.
func (m *model) beginFilter() {
	origin := m.hoveredFilterKey()

	var opts []filter.Option
	if screen, ok := m.active().(queryGrammarer); ok && !m.isDetailsFocused(m.inputs()) {
		if grammar, reads := screen.QueryGrammar(); reads {
			opts = append(opts, filter.WithGrammar(grammar, m.clock))
		}
	}

	search := &m.filter
	search.Open(opts...)
	search.OpenedIn = m.keyholder(m.inputs())
	search.Origin = origin
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
		m.filter = filter.State{}
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
		b.BlurBlock()
	}
	m.reading.focused = false

	if l, ok := m.active().(screenLeaver); ok {
		return m.apply(l.LeaveScreen())
	}

	return nil
}

// clearFilter drops a standing search and puts the cursor back where the search opened, unless
// the keys have since moved to the other section.
func (m *model) clearFilter() {
	search := &m.filter
	origin, openedIn := search.Origin, search.OpenedIn
	search.Abandon()
	search.Origin = ""
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
	search := &m.filter
	switch key.String() {
	case "tab":
		search.Mode = search.Mode.Flipped()
	case "enter":
		m.applyFilter()

		return tea.Batch(m.waitToStage(), m.saysCmd())
	default:
		search.TakeKey(key, m.register)
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
	m.filter.Typing = false
	if !ok {
		found = ""
	}
	if m.filter.Pattern().IsNarrowing() && m.isFilterRowShown(hovered) {
		found = hovered
	}

	m.placeFilteredCursor(found)
	m.filter.AppliedAt = m.hoveredFilterKey()
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

	return l.Hovered(m.inputs())
}

func (m *model) placeFilteredCursor(hovered string) {
	if l, ok := m.filteredList(); ok {
		m.apply(l.Place(m.inputs(), hovered))
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
	pattern := m.filter.Pattern()
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

func (m *model) clickSearch(key string) tea.Cmd {
	to, ok := searchStops[key]
	if !ok {
		return nil
	}

	if m.isViewsOpen() {
		m.closeViews()
	}

	entered := m.enterStop(to)
	if m.isTyping() {
		return entered
	}

	return tea.Batch(entered, m.route(keys.Press(filter.OpenKey)))
}

// filterBy narrows the list up as if the user typed and applied the query, in the grammar the
// screen up reads.
func (m *model) filterBy(query string) {
	var opts []filter.Option
	if s, ok := m.active().(queryGrammarer); ok {
		if grammar, reads := s.QueryGrammar(); reads {
			opts = append(opts, filter.WithGrammar(grammar, m.clock))
		}
	}

	m.filter.Open(opts...)
	m.filter.Mode = filter.ModeFilter
	m.filter.OpenedIn = list.SectionList
	m.filter.SetQuery(query)
	m.applyFilter()
}
