package ui

import (
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/zone"
	"komrad/internal/view"
	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// screenAction is one arm of a screen's dispatch, which moves through nav or acts as a declared direct exception.
type screenAction struct {
	id     actionID
	keys   func(config.Keybindings) []string
	when   func(*model) bool
	nav    func(*model, string) tea.Cmd
	direct *directAction
}

// directAction is an arm that acts straight off its key rather than from the menu, and why.
type directAction struct {
	reason string
	run    func(*model, string) tea.Cmd
}

// screenLegendRow is one row of a screen's legend.
type screenLegendRow struct {
	of    []actionID
	desc  func(*model) string
	show  func(*model) bool
	label func(config.Keybindings) string
	strip bool
}

// screenActionSpec is a screen's keys, ordered twice: the table in precedence order, the
// dispatch order, and the legend in reading order.
type screenActionSpec struct {
	table  []screenAction
	legend []screenLegendRow
}

// dispatch hands the press to the first arm that answers it.
func (s screenActionSpec) dispatch(m *model, pressed string) tea.Cmd {
	for _, a := range s.table {
		if a.when != nil && !a.when(m) {
			continue
		}

		if !isBound(pressed, a.keys(m.keys)) {
			continue
		}

		if a.direct != nil {
			return a.direct.run(m, pressed)
		}

		return a.nav(m, pressed)
	}

	return nil
}

// help builds the legend from the same table the dispatch reads.
func (s screenActionSpec) help(m *model) keyMap {
	var short, full []key.Binding

	for _, row := range s.legend {
		if !s.isRowShown(m, row) {
			continue
		}

		binding := s.rowBinding(m, row)
		full = append(full, binding)

		if row.strip {
			short = append(short, binding)
		}
	}

	return keyMap{short: short, full: full}
}

func (s screenActionSpec) isRowShown(m *model, row screenLegendRow) bool {
	if row.show != nil {
		return row.show(m)
	}

	return slices.ContainsFunc(s.table, func(a screenAction) bool {
		return slices.Contains(row.of, a.id) && (a.when == nil || a.when(m))
	})
}

// rowBinding converts the row into the format bubbleTea wants it: every key of every action it
// names, under one label.
func (s screenActionSpec) rowBinding(m *model, row screenLegendRow) key.Binding {
	var keys, heads []string

	for _, id := range row.of {
		a, ok := s.answeringActionOf(m, id)
		if !ok {
			continue
		}

		bound := a.keys(m.keys)
		keys = append(keys, bound...)
		heads = append(heads, headLabel(bound))
	}

	label := strings.Join(heads, "/")
	if len(row.of) == 1 {
		label = keyLabel(keys)
	}

	if row.label != nil {
		label = row.label(m.keys)
	}

	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, row.desc(m)))
}

// answeringActionOf is the first arm with the id that answers now, or the first arm with it.
func (s screenActionSpec) answeringActionOf(m *model, id actionID) (screenAction, bool) {
	i := slices.IndexFunc(s.table, func(a screenAction) bool {
		return a.id == id && (a.when == nil || a.when(m))
	})
	if i < 0 {
		return s.actionOf(id)
	}

	return s.table[i], true
}

func (s screenActionSpec) actionOf(id actionID) (screenAction, bool) {
	i := slices.IndexFunc(s.table, func(a screenAction) bool { return a.id == id })
	if i < 0 {
		return screenAction{}, false
	}

	return s.table[i], true
}

// dispatchArms is the table as the keybinding check walks it.
func (s screenActionSpec) dispatchArms(keys config.Keybindings) []dispatchArm {
	return xslice.Map(s.table, func(a screenAction) dispatchArm {
		return dispatchArm{
			describe: "an action of the screen's own",
			keys:     a.keys(keys),
			guarded:  a.when != nil,
		}
	})
}

// screenList is what the keys every list shares need of one screen: where its cursor and
// rows live, and what follows the cursor onto a row.
type screenList[T list.Keyed] struct {
	cursor func() *list.Cursor
	rows   func(*model) []T
	// name is the text a search reads a row by, nil on a list that renders no filter.
	name func(T) string
	// fits is how a row meets a qualifier, nil on a list that reads none.
	fits func(*model, T) clauseFits
	// refs is the rows as the selection sees them, nil on a list nothing can be selected in.
	refs func([]T) []list.Ref
	// header is what the toggle key names a header row as.
	header string
	// placed follows the cursor onto a row at once, when set.
	placed func(*model)
	// moved follows the cursor onto a row a key put it on, and is placed when nil.
	moved func(*model) tea.Cmd
}

func (l screenList[T]) isSelecting(*model) bool {
	return l.cursor().Selected.IsActive()
}

func (l screenList[T]) isBrowsing(m *model) bool {
	return !l.isSelecting(m)
}

// hovered is the key of the row the cursor is on.
func (l screenList[T]) hovered(m *model) string {
	return list.KeyAt(l.rows(m), l.cursor().Row)
}

func (l screenList[T]) hoveredName(m *model) string {
	row, ok := list.At(l.rows(m), l.cursor().Row)
	if !ok || l.name == nil {
		return ""
	}

	return l.name(row)
}

// place puts the cursor back on a row by key, or as near to where it was as the list allows.
func (l screenList[T]) place(m *model, hovered string) {
	rows := l.rows(m)
	c := l.cursor()
	c.Row = cursor.Place(list.IndexOf(rows, hovered), c.Row, len(rows))
	if l.placed != nil {
		l.placed(m)
	}
}

func (l screenList[T]) filterRows(m *model) []filterRow {
	return xslice.Map(l.rows(m), func(row T) filterRow {
		var fits clauseFits
		if l.fits != nil {
			fits = l.fits(m, row)
		}

		return filterRow{
			key:  row.Key(),
			name: l.name(row),
			fits: fits,
		}
	})
}

func (l screenList[T]) landed(m *model) tea.Cmd {
	if l.moved != nil {
		return l.moved(m)
	}

	if l.placed != nil {
		l.placed(m)
	}

	return nil
}

func (l screenList[T]) move(m *model, delta int) tea.Cmd {
	next, ok := l.cursor().Move(delta, len(l.rows(m)))
	if !ok {
		return nil
	}

	*l.cursor() = next

	return l.landed(m)
}

func (l screenList[T]) jump(m *model, idx int) tea.Cmd {
	next, ok := l.cursor().Jump(idx, len(l.rows(m)))
	if !ok {
		return nil
	}

	*l.cursor() = next

	return l.landed(m)
}

func (l screenList[T]) click(m *model, key string, nth int) tea.Cmd {
	for i, row := range l.rows(m) {
		if row.Key() != key {
			continue
		}

		if nth > 0 {
			nth--

			continue
		}

		return l.jump(m, i)
	}

	return nil
}

// update hands a key to the list's table.
func (l screenList[T]) update(m *model, key tea.KeyPressMsg, actions screenActionSpec) tea.Cmd {
	return actions.dispatchTop(m, key.String())
}

// actions are the keys every list answers, in dispatch order.
func (l screenList[T]) actions() []screenAction {
	var shared []screenAction
	if l.refs != nil {
		shared = append(shared, l.selectionActions()...)
	}
	shared = append(shared, screenAction{
		id:   actionQuit,
		keys: fixedQuit,
		nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
	})
	shared = append(shared, navActions(l, navSteps{page: navPageStep, scroll: 1})...)
	if l.name != nil {
		shared = append(shared, l.filterActions()...)
	}

	return shared
}

func (l screenList[T]) selectionActions() []screenAction {
	return []screenAction{
		{
			id:   actionSelectClear,
			keys: func(k config.Keybindings) []string { return k.Selection.Clear },
			when: l.isSelecting,
			nav:  l.clearSelection,
		},
		{
			id:   actionSelectToggle,
			keys: func(k config.Keybindings) []string { return k.Selection.Toggle },
			nav:  l.toggleSelection,
		},
		{
			id:   actionSelectRange,
			keys: func(k config.Keybindings) []string { return k.Selection.Range },
			nav:  l.extendSelection,
		},
	}
}

func (l screenList[T]) filterActions() []screenAction {
	standing := func(m *model) bool { return l.isBrowsing(m) && m.filter.isStanding() }

	return []screenAction{
		{
			id:   actionFilterOpen,
			keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
			when: l.isBrowsing,
			nav: func(m *model, _ string) tea.Cmd {
				m.beginFilter()

				return nil
			},
		},
		{
			id:   actionFilterMatch,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
			when: standing,
			nav:  func(m *model, pressed string) tea.Cmd { return m.jumpToFilterMatch(pressed) },
		},
		{
			id:   actionFilterClear,
			keys: fixedBack,
			when: standing,
			nav: func(m *model, _ string) tea.Cmd {
				m.clearFilter()

				return m.waitToStage()
			},
		},
	}
}

func (l screenList[T]) moveBy(m *model, rows int) tea.Cmd {
	if rows == 1 || rows == -1 {
		return l.move(m, rows)
	}

	return l.jump(m, l.cursor().Row+rows)
}

func (l screenList[T]) moveToEnd(m *model, direction int) tea.Cmd {
	if direction > 0 {
		return l.jump(m, len(l.rows(m))-1)
	}

	return l.jump(m, 0)
}

func (l screenList[T]) clearSelection(*model, string) tea.Cmd {
	l.cursor().Selected = list.Selection{}

	return nil
}

func (l screenList[T]) toggleSelection(m *model, _ string) tea.Cmd {
	c := l.cursor()
	c.Selected = c.Selected.Toggle(l.refs(l.rows(m)), c.Row)

	return nil
}

func (l screenList[T]) extendSelection(m *model, pressed string) tea.Cmd {
	if !l.isSelecting(m) {
		return l.toggleSelection(m, pressed)
	}

	c := l.cursor()
	c.Selected = c.Selected.Extend(l.refs(l.rows(m)), c.Row)

	return nil
}

// selectionLegend is the three rows a standing selection is named by.
func (l screenList[T]) selectionLegend() []screenLegendRow {
	return []screenLegendRow{
		{
			of:    []actionID{actionSelectToggle},
			desc:  l.toggleDesc,
			show:  l.isSelecting,
			strip: true,
		},
		{
			of:   []actionID{actionSelectRange},
			desc: func(*model) string { return "select to nearest" },
			show: l.isSelecting,
		},
		{
			of:    []actionID{actionSelectClear},
			desc:  l.clearDesc,
			show:  l.isSelecting,
			strip: true,
		},
	}
}

func (l screenList[T]) toggleDesc(m *model) string {
	c := l.cursor()
	hovered, _ := list.At(l.refs(l.rows(m)), c.Row)

	switch {
	case hovered.Header:
		return "select " + l.header
	case c.Selected.Has(hovered.Key):
		return "deselect"
	}

	return "select"
}

func (l screenList[T]) clearDesc(*model) string {
	return fmt.Sprintf("clear %d selected", len(l.cursor().Selected.Keys))
}

// screenNavLegend is the four rows the cursor keys are named by.
func screenNavLegend() []screenLegendRow {
	return []screenLegendRow{
		{
			of:    []actionID{actionMove},
			desc:  func(*model) string { return "move" },
			label: moveLabel,
		},
		{
			of:    []actionID{actionJump},
			desc:  func(*model) string { return "top/bottom" },
			label: jumpLabel,
		},
		{
			of:    []actionID{actionNavPage},
			desc:  func(*model) string { return "page" },
			label: navPageLabel,
		},
		{
			of:    []actionID{actionColumnScroll},
			desc:  func(*model) string { return "scroll" },
			label: columnScrollLabel,
		},
	}
}

// filterDescOf names the filter key by the search standing in the list.
func filterDescOf(m *model) string {
	return m.filter.openDesc()
}

func filterClearDescOf(m *model) string {
	return m.filter.clearDesc()
}

// screenListParams is a screen's list as the root renders it: its title and tally, its own
// legend, and the rows the window is filled from.
type screenListParams struct {
	title string
	tally string
	// help is the screen's own legend, rendered ahead of the bindings every list shares.
	help func(*model) keyMap
	// empty is what the list says over no rows, and blank on a list that renders its window anyway.
	empty string
	// rows is how many rows the list holds.
	rows     int
	snapshot func(list.Pane) view.Snapshot
}

// listHelpOf is a screen's legend as the root renders it under the list.
func (m *model) listHelpOf(params screenListParams) keyMap {
	return m.listHelp(params.help(m))
}

// listHelp is a list screen's legend: its own rows, then the bindings every list shares.
func (m *model) listHelp(own keyMap) keyMap {
	// A disabled binding renders nothing but still takes a cell of the expanded legend.
	var tabs []key.Binding
	if b := m.tabsBinding(); b.Enabled() {
		tabs = []key.Binding{b}
	}
	if b := m.kindsBinding(); b.Enabled() {
		tabs = append(tabs, b)
	}

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full: slices.Concat(
			own.full,
			tabs,
			m.fullGlobalHelp(),
		),
	}
}

// navPageStep is how many rows a page key moves the cursor.
const navPageStep = 10

// listLines is the room the list has once the chrome around it has taken its share.
func (m *model) listLines(chrome ...string) int {
	return fitLines(m.term.height, chrome...)
}

// fitLines is the room a pane of height lines has once the chrome has taken its share.
func fitLines(height int, chrome ...string) int {
	if height <= 0 {
		return paint.Unbounded
	}

	return max(spareLinesOf(height, chrome...), 0)
}

// spareLines is what the pane has left once the chrome has taken its share, unclamped.
func (m *model) spareLines(chrome ...string) int {
	return spareLinesOf(m.term.height, chrome...)
}

// spareLinesOf is what a pane of height lines has left once the chrome has taken its share.
func spareLinesOf(height int, chrome ...string) int {
	if height <= 0 {
		return 0
	}

	lines := height
	for _, part := range chrome {
		lines -= strings.Count(part, "\n")
	}

	return lines
}

// directly declares an arm that acts straight off its key, and why it is not in the menu.
func directly(reason string, run func(*model, string) tea.Cmd) *directAction {
	return &directAction{reason: reason, run: run}
}

// notInMenu is the reason on an action arm that has not moved into the menu yet.
const notInMenu = "not yet moved into the menu"

func (m *model) listClicker(l keyedList) clicker {
	if !m.isListFocused() || m.isViewsOpen() {
		return m.stopClicker(stopList, l)
	}

	return clickFunc(func(m *model, key string, nth int) tea.Cmd {
		if key != l.hovered(m) {
			return l.click(m, key, nth)
		}

		menu, ok := m.buildMenu()
		if !ok {
			return nil
		}

		return openSubmenu(m, menu)
	})
}

func (m *model) isHoveredHit(hit zone.Hit) bool {
	lister, ok := m.active().(activeLister)

	return ok && hit.Zone == zoneList && hit.Key == lister.activeList().hovered(m)
}
