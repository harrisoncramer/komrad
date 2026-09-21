package ui

import (
	"fmt"
	"slices"
	"strings"

	"weave/config"
	"weave/internal/view"
	"weave/lib/xslice"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// screenAction is one arm of a moved screen's dispatch, closed over the screen it belongs to.
type screenAction struct {
	id   actionID
	keys func(config.Keybindings) []string
	when func(panelShared) bool
	run  func(host, panelShared, string) tea.Cmd
}

// screenLegendRow is one row of a moved screen's legend.
type screenLegendRow struct {
	of    []actionID
	desc  func(panelShared) string
	show  func(panelShared) bool
	label func(config.Keybindings) string
	strip bool
}

// screenActionSpec is a moved screen's keys, ordered twice: the table in precedence order, the
// dispatch order, and the legend in reading order.
type screenActionSpec struct {
	table  []screenAction
	legend []screenLegendRow
}

// dispatch hands the press to the first arm that answers it.
func (s screenActionSpec) dispatch(h host, shared panelShared, pressed string) tea.Cmd {
	for _, a := range s.table {
		if a.when != nil && !a.when(shared) {
			continue
		}

		if !isBound(pressed, a.keys(shared.keys)) {
			continue
		}

		return a.run(h, shared, pressed)
	}

	return nil
}

// help builds the legend from the same table the dispatch reads.
func (s screenActionSpec) help(shared panelShared) keyMap {
	var short, full []key.Binding

	for _, row := range s.legend {
		if !s.isRowShown(shared, row) {
			continue
		}

		binding := s.rowBinding(shared, row)
		full = append(full, binding)

		if row.strip {
			short = append(short, binding)
		}
	}

	return keyMap{short: short, full: full}
}

func (s screenActionSpec) isRowShown(shared panelShared, row screenLegendRow) bool {
	if row.show != nil {
		return row.show(shared)
	}

	return slices.ContainsFunc(row.of, func(id actionID) bool {
		a, ok := s.actionOf(id)

		return ok && (a.when == nil || a.when(shared))
	})
}

// rowBinding converts the row into the format bubbleTea wants it: every key of every action it
// names, under one label.
func (s screenActionSpec) rowBinding(shared panelShared, row screenLegendRow) key.Binding {
	var keys, heads []string

	for _, id := range row.of {
		a, ok := s.actionOf(id)
		if !ok {
			continue
		}

		bound := a.keys(shared.keys)
		keys = append(keys, bound...)
		heads = append(heads, headLabel(bound))
	}

	label := strings.Join(heads, "/")
	if len(row.of) == 1 {
		label = keyLabel(keys)
	}

	if row.label != nil {
		label = row.label(shared.keys)
	}

	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, row.desc(shared)))
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

// screenList is what the keys every list shares need of one moved screen: where its cursor and
// rows live, and what follows the cursor onto a row.
type screenList[T keyed] struct {
	cursor func() *rowCursor
	rows   func(panelShared) []T
	// name is the text a search reads a row by, nil on a list that draws no filter.
	name func(T) string
	// refs is the rows as the selection sees them, nil on a list nothing can be selected in.
	refs func([]T) []rowRef
	// header is what the toggle key names a header row as.
	header string
	// placed follows the cursor onto a row at once.
	placed func(host, panelShared)
	// moved follows the cursor onto a row a key put it on, and is placed when nil.
	moved func(host, panelShared) tea.Cmd
	quit  func(config.Keybindings) []string
	back  func(config.Keybindings) []string
}

func (l screenList[T]) isSelecting(panelShared) bool {
	return l.cursor().selected.isActive()
}

func (l screenList[T]) isBrowsing(s panelShared) bool {
	return !l.isSelecting(s)
}

// hovered is the key of the row the cursor is on.
func (l screenList[T]) hovered(s panelShared) string {
	return rowKeyAt(l.rows(s), l.cursor().cursor)
}

// place puts the cursor back on a row by key, or as near to where it was as the list allows.
func (l screenList[T]) place(h host, s panelShared, hovered string) {
	rows := l.rows(s)
	c := l.cursor()
	c.cursor = placeCursor(rowIndexOfKey(rows, hovered), c.cursor, len(rows))
	l.placed(h, s)
}

func (l screenList[T]) filterRows(s panelShared) []filterRow {
	return xslice.Map(l.rows(s), func(row T) filterRow {
		return filterRow{key: row.key(), name: l.name(row)}
	})
}

func (l screenList[T]) landed(h host, s panelShared) tea.Cmd {
	if l.moved != nil {
		return l.moved(h, s)
	}

	l.placed(h, s)

	return nil
}

// move steps the cursor, and a step refused at the bottom walks on into the record.
func (l screenList[T]) move(h host, s panelShared, delta int) tea.Cmd {
	next, ok := l.cursor().move(delta, len(l.rows(s)))
	if !ok {
		if delta > 0 {
			return h.stepIntoDetails()
		}

		return nil
	}

	*l.cursor() = next

	return l.landed(h, s)
}

func (l screenList[T]) jump(h host, s panelShared, idx int) tea.Cmd {
	next, ok := l.cursor().jump(idx, len(l.rows(s)))
	if !ok {
		return nil
	}

	*l.cursor() = next

	return l.landed(h, s)
}

// update settles the doubled top key ahead of the table.
func (l screenList[T]) update(h host, s panelShared, key tea.KeyMsg, actions screenActionSpec) tea.Cmd {
	k := key.String()

	if h.takeTopPress(k) {
		return l.jump(h, s, 0)
	}
	if h.isTopHeld() {
		return nil
	}

	return actions.dispatch(h, s, k)
}

// actions are the keys every list answers, in dispatch order.
func (l screenList[T]) actions() []screenAction {
	shared := []screenAction{
		{
			id:   actionJump,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Top, k.Nav.Bottom) },
			run:  l.jumpPressed,
		},
	}
	if l.refs != nil {
		shared = append(shared, l.selectionActions()...)
	}
	shared = append(shared,
		screenAction{
			id:   actionQuit,
			keys: l.quit,
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.hide() },
		},
		screenAction{
			id:   actionMove,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Up, k.Nav.Down) },
			run:  l.movePressed,
		},
		screenAction{
			id:   actionNavPage,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.PageUp, k.Nav.PageDown) },
			run:  l.pagePressed,
		},
	)
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
			run:  l.clearSelection,
		},
		{
			id:   actionSelectToggle,
			keys: func(k config.Keybindings) []string { return k.Selection.Toggle },
			run:  l.toggleSelection,
		},
		{
			id:   actionSelectRange,
			keys: func(k config.Keybindings) []string { return k.Selection.Range },
			run:  l.extendSelection,
		},
	}
}

func (l screenList[T]) filterActions() []screenAction {
	standing := func(s panelShared) bool { return l.isBrowsing(s) && s.chrome.filter.isStanding() }

	return []screenAction{
		{
			id:   actionFilterOpen,
			keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
			when: l.isBrowsing,
			run: func(h host, _ panelShared, _ string) tea.Cmd {
				h.beginFilter()

				return nil
			},
		},
		{
			id:   actionFilterMatch,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Filter.NextMatch, k.Filter.PrevMatch) },
			when: standing,
			run:  func(h host, _ panelShared, pressed string) tea.Cmd { return h.jumpToFilterMatch(pressed) },
		},
		{
			id:   actionFilterClear,
			keys: l.back,
			when: standing,
			run: func(h host, _ panelShared, _ string) tea.Cmd {
				h.clearFilter()

				return nil
			},
		},
	}
}

// jumpPressed answers the bottom key.
func (l screenList[T]) jumpPressed(h host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Nav.Bottom) {
		return l.jump(h, s, len(l.rows(s))-1)
	}

	return nil
}

func (l screenList[T]) pagePressed(h host, s panelShared, pressed string) tea.Cmd {
	return l.jump(h, s, l.cursor().cursor+navPageDeltaOf(s.keys, pressed))
}

func (l screenList[T]) movePressed(h host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Nav.Down) {
		return l.move(h, s, 1)
	}

	return l.move(h, s, -1)
}

func (l screenList[T]) clearSelection(host, panelShared, string) tea.Cmd {
	l.cursor().selected = selection{}

	return nil
}

func (l screenList[T]) toggleSelection(_ host, s panelShared, _ string) tea.Cmd {
	c := l.cursor()
	c.selected = c.selected.toggle(l.refs(l.rows(s)), c.cursor)

	return nil
}

func (l screenList[T]) extendSelection(h host, s panelShared, pressed string) tea.Cmd {
	if !l.isSelecting(s) {
		return l.toggleSelection(h, s, pressed)
	}

	c := l.cursor()
	c.selected = c.selected.extend(l.refs(l.rows(s)), c.cursor)

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
			desc: func(panelShared) string { return "select to nearest" },
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

func (l screenList[T]) toggleDesc(s panelShared) string {
	c := l.cursor()
	hovered, _ := rowAt(l.refs(l.rows(s)), c.cursor)

	switch {
	case hovered.header:
		return "select " + l.header
	case c.selected.has(hovered.key):
		return "deselect"
	}

	return "select"
}

func (l screenList[T]) clearDesc(panelShared) string {
	return fmt.Sprintf("clear %d selected", len(l.cursor().selected.keys))
}

// screenNavLegend is the three rows the cursor keys are named by.
func screenNavLegend() []screenLegendRow {
	return []screenLegendRow{
		{
			of:    []actionID{actionMove},
			desc:  func(panelShared) string { return "move" },
			label: moveLabel,
		},
		{
			of:    []actionID{actionJump},
			desc:  func(panelShared) string { return "top/bottom" },
			label: jumpLabel,
		},
		{
			of:    []actionID{actionNavPage},
			desc:  func(panelShared) string { return "page" },
			label: navPageLabel,
		},
	}
}

// filterDescOf names the filter key by the search standing in the list.
func filterDescOf(s panelShared) string {
	if s.chrome.filter.isStanding() {
		return s.chrome.filter.mode.String() + ": " + s.chrome.filter.query
	}

	return "search"
}

func filterClearDescOf(s panelShared) string {
	return "clear " + s.chrome.filter.mode.String()
}

// screenListParams is a moved screen's list as the root draws it: its title and tally, its own
// legend, and the rows the window is filled from.
type screenListParams struct {
	title string
	tally string
	// help is the screen's own legend, drawn ahead of the bindings every list shares.
	help func(panelShared) keyMap
	// column names what the scroll keys move, for the legend.
	column string
	// empty is what the list says over no rows, and blank on a list that draws its window anyway.
	empty string
	// rows is how many rows the list holds.
	rows     int
	snapshot func(listPane) view.Snapshot
	// lines is the list drawn by the screen itself, row by row with its cursor, on a screen that
	// draws no snapshot.
	lines func(listPane) ([]string, int)
}

// listPane is the column a list is drawn in this frame and how its cursor stands.
type listPane struct {
	width      int
	caret      view.Caret
	blinkedOut bool
}

// drawer is the frame the root draws a moved screen's list, record and legend in.
type drawer interface {
	// columnListView draws the list beside the screen's own block, or the list alone on a window
	// that will not hold both.
	columnListView(params screenListParams) string
	listFitsTabs(params screenListParams) bool
	listFitsDetails(params screenListParams) bool
	listRecordRoom(params screenListParams) int

	// The pieces a screen composing its own frame draws with.
	screenColumnSplit() (columnSplit, bool)
	joinColumns(split columnSplit, sidebar, column string) string
	renderHelp(k keyMap, above ...string) string
	globalHelp() []key.Binding
	detailsBinding() key.Binding
	listLines(chrome ...string) int
	listPane(width int) listPane
	detailsView() string
	messagesRegion(record, help string, above ...string) string

	// The record under a screen composing its own sidebar, measured against the legend and the
	// head that screen draws.
	sidebarFitsDetails(help func(panelShared) keyMap, head string) bool
	sidebarDetailsRoom(help func(panelShared) keyMap, head string) int

	// Where the keys are, as the screen's own block beside the list reads it.
	isListFocused() bool
	isDetailsFocused() bool
}

var _ drawer = (*model)(nil)

// listPane is the column a list is drawn in this frame and how its cursor stands.
func (m model) listPane(width int) listPane {
	return listPane{width: width, caret: m.caretOf(sectionList), blinkedOut: m.rowCursor.Blink}
}

func (m model) columnListView(params screenListParams) string {
	return m.columnView(func(m model) string { return m.screenListView(params) })
}

// screenListView draws a moved screen's list under its header with its record and its legend
// under it.
func (m model) screenListView(params screenListParams) string {
	chrome := m.listChromeOf(params)
	if params.lines == nil {
		return m.listView(listViewParams{
			chrome: chrome,
			empty:  params.empty,
			rows:   params.rows,
			snapshot: func(m model, pane int) view.Snapshot {
				return params.snapshot(m.listPane(pane))
			},
		})
	}

	header := chrome.header(m)
	record, help := m.detailsView(), m.renderHelp(chrome.help(m), header)
	rows, cursor := params.lines(m.listPane(m.paneWidth()))

	return m.headedListView(header, rows, cursor, record+m.messagesRegion(record, help, header)+help)
}

func (m model) listFitsTabs(params screenListParams) bool {
	return m.listChromeOf(params).doesFitTabs(m)
}

func (m model) listFitsDetails(params screenListParams) bool {
	return m.listChromeOf(params).doesFitDetails(m)
}

func (m model) listRecordRoom(params screenListParams) int {
	return m.listChromeOf(params).detailsRoom(m)
}

// sidebarFitsDetails is whether the record fits under the head, judged with the keys on the
// screen.
func (m model) sidebarFitsDetails(help func(panelShared) keyMap, head string) bool {
	m.details.focused = false

	return m.spareLines(m.renderHelp(help(m.shared())), head) > m.detailsLines()+detailsChromeLines
}

// sidebarDetailsRoom is the rows the sidebar has under the head and the legend it is drawing.
func (m model) sidebarDetailsRoom(help func(panelShared) keyMap, head string) int {
	return m.spareLines(m.renderHelp(help(m.shared())), head)
}

// listChromeOf is a moved screen's list as the root's own chrome reads it.
func (m model) listChromeOf(params screenListParams) listChrome {
	return listChrome{
		title: params.title,
		tally: func(model) string { return params.tally },
		help:  func(m model) keyMap { return m.listHelp(params.help(m.shared()), params.column) },
	}
}

// listHelp is a list screen's legend: its own rows, then the bindings every list shares.
func (m model) listHelp(own keyMap, column string) keyMap {
	// A disabled binding draws nothing but still takes a cell of the expanded legend.
	var tabs []key.Binding
	if b := m.tabsBinding(); b.Enabled() {
		tabs = []key.Binding{b}
	}

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full: slices.Concat(
			own.full,
			tabs,
			[]key.Binding{
				m.detailsBinding(),
				m.messagesBinding(),
			},
			m.infoPaneHelp(),
			[]key.Binding{m.columnScrollBinding(column)},
			m.globalHelp(),
		),
	}
}
