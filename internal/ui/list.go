package ui

import (
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/zone"
	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// screenList is what the keys every list shares need of one screen: where its cursor and
// rows live, and what follows the cursor onto a row.
type screenList[T list.Keyed] struct {
	keys   config.Keybindings
	top    *keys.Top
	cursor func() *list.Cursor
	rows   func(*model, screen.Inputs) []T
	// name is the text a search reads a row by, nil on a list that renders no filter.
	name func(T) string
	// fits is how a row meets a qualifier, nil on a list that reads none.
	fits func(*model, panel.Listing) func(T) clauseFits
	// refs is the rows as the selection sees them, nil on a list nothing can be selected in.
	refs func([]T) []list.Ref
	// header is what the toggle key names a header row as.
	header string
	// placed follows the cursor onto a row at once, when set.
	placed func(*model, screen.Inputs) screen.Request
	// moved follows the cursor onto a row a key put it on, and is placed when nil.
	moved func(*model, screen.Inputs) screen.Request
}

func (l screenList[T]) isSelecting(*model) bool {
	return l.cursor().Selected.IsActive()
}

func (l screenList[T]) isBrowsing(m *model) bool {
	return !l.isSelecting(m)
}

// hovered is the key of the row the cursor is on.
func (l screenList[T]) hovered(m *model, in screen.Inputs) string {
	return list.KeyAt(l.rows(m, in), l.cursor().Row)
}

func (l screenList[T]) hoveredName(m *model, in screen.Inputs) string {
	row, ok := list.At(l.rows(m, in), l.cursor().Row)
	if !ok || l.name == nil {
		return ""
	}

	return l.name(row)
}

// place puts the cursor back on a row by key, or as near to where it was as the list allows.
func (l screenList[T]) place(m *model, in screen.Inputs, hovered string) screen.Request {
	rows := l.rows(m, in)
	c := l.cursor()
	c.Row = cursor.Place(list.IndexOf(rows, hovered), c.Row, len(rows))
	if l.placed != nil {
		return l.placed(m, in)
	}

	return nil
}

func (l screenList[T]) filterRows(m *model, in screen.Inputs) []filterRow {
	var fits func(T) clauseFits
	if l.fits != nil {
		fits = l.fits(m, in.Listing)
	}

	return xslice.Map(l.rows(m, in), func(row T) filterRow {
		var rowFits clauseFits
		if fits != nil {
			rowFits = fits(row)
		}

		return filterRow{
			key:  row.Key(),
			name: l.name(row),
			fits: rowFits,
		}
	})
}

func (l screenList[T]) landed(m *model, in screen.Inputs) screen.Request {
	if l.moved != nil {
		return l.moved(m, in)
	}

	if l.placed != nil {
		return l.placed(m, in)
	}

	return nil
}

func (l screenList[T]) move(m *model, in screen.Inputs, delta int) screen.Request {
	next, ok := l.cursor().Move(delta, len(l.rows(m, in)))
	if !ok {
		return nil
	}

	*l.cursor() = next

	return l.landed(m, in)
}

func (l screenList[T]) jump(m *model, in screen.Inputs, idx int) screen.Request {
	next, ok := l.cursor().Jump(idx, len(l.rows(m, in)))
	if !ok {
		return nil
	}

	*l.cursor() = next

	return l.landed(m, in)
}

func (l screenList[T]) click(m *model, in screen.Inputs, key string, nth int) screen.Request {
	for i, row := range l.rows(m, in) {
		if row.Key() != key {
			continue
		}

		if nth > 0 {
			nth--

			continue
		}

		return l.jump(m, in, i)
	}

	return nil
}

// update hands a key to the list's table.
func (l screenList[T]) update(m *model, key tea.KeyPressMsg, actions keys.Spec[*model]) screen.Request {
	return actions.DispatchTop(l.top, m, l.keys, key.String())
}

// actions are the keys every list answers, in dispatch order.
func (l screenList[T]) actions() []keys.Action[*model] {
	var shared []keys.Action[*model]
	if l.refs != nil {
		shared = append(shared, l.selectionActions()...)
	}
	shared = append(shared, keys.Action[*model]{
		ID:   keys.Quit,
		Keys: keys.FixedQuit,
		Nav:  func(*model, string) screen.Request { return screen.Hide{} },
	})
	shared = append(shared, navActions(l, navSteps{page: navPageStep, scroll: 1})...)
	if l.name != nil {
		shared = append(shared, l.filterActions()...)
	}

	return shared
}

func (l screenList[T]) selectionActions() []keys.Action[*model] {
	return []keys.Action[*model]{
		{
			ID:   keys.SelectClear,
			Keys: func(k config.Keybindings) []string { return k.Selection.Clear },
			When: l.isSelecting,
			Nav:  l.clearSelection,
		},
		{
			ID:   keys.SelectToggle,
			Keys: func(k config.Keybindings) []string { return k.Selection.Toggle },
			Nav:  l.toggleSelection,
		},
		{
			ID:   keys.SelectRange,
			Keys: func(k config.Keybindings) []string { return k.Selection.Range },
			Nav:  l.extendSelection,
		},
	}
}

func (l screenList[T]) filterActions() []keys.Action[*model] {
	standing := func(m *model) bool { return l.isBrowsing(m) && m.filter.isStanding() }

	return []keys.Action[*model]{
		{
			ID:   keys.FilterOpen,
			Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
			When: l.isBrowsing,
			Nav:  func(*model, string) screen.Request { return screen.BeginFilter{} },
		},
		{
			ID:   keys.FilterMatch,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
			When: standing,
			Nav: func(_ *model, pressed string) screen.Request {
				return screen.StepMatch{Delta: keys.DirectionOf(pressed, l.keys.Filter.PrevMatch)}
			},
		},
		{
			ID:   keys.FilterClear,
			Keys: keys.FixedBack,
			When: standing,
			Nav:  func(*model, string) screen.Request { return screen.Batch{screen.ClearFilter{}, screen.Settle{}} },
		},
	}
}

func (l screenList[T]) moveBy(m *model, rows int) screen.Request {
	in := m.inputs()
	if rows == 1 || rows == -1 {
		return l.move(m, in, rows)
	}

	return l.jump(m, in, l.cursor().Row+rows)
}

func (l screenList[T]) moveToEnd(m *model, direction int) screen.Request {
	in := m.inputs()
	if direction > 0 {
		return l.jump(m, in, len(l.rows(m, in))-1)
	}

	return l.jump(m, in, 0)
}

func (l screenList[T]) clearSelection(*model, string) screen.Request {
	l.cursor().Selected = list.Selection{}

	return nil
}

func (l screenList[T]) toggleSelection(m *model, _ string) screen.Request {
	c := l.cursor()
	c.Selected = c.Selected.Toggle(l.refs(l.rows(m, m.inputs())), c.Row)

	return nil
}

func (l screenList[T]) extendSelection(m *model, pressed string) screen.Request {
	if !l.isSelecting(m) {
		return l.toggleSelection(m, pressed)
	}

	c := l.cursor()
	c.Selected = c.Selected.Extend(l.refs(l.rows(m, m.inputs())), c.Row)

	return nil
}

// selectionLegend is the three rows a standing selection is named by.
func (l screenList[T]) selectionLegend() []keys.LegendRow[*model] {
	return []keys.LegendRow[*model]{
		{
			Of:    []keys.ID{keys.SelectToggle},
			Desc:  l.toggleDesc,
			Show:  l.isSelecting,
			Strip: true,
		},
		{
			Of:   []keys.ID{keys.SelectRange},
			Desc: func(*model) string { return "select to nearest" },
			Show: l.isSelecting,
		},
		{
			Of:    []keys.ID{keys.SelectClear},
			Desc:  l.clearDesc,
			Show:  l.isSelecting,
			Strip: true,
		},
	}
}

func (l screenList[T]) toggleDesc(m *model) string {
	c := l.cursor()
	hovered, _ := list.At(l.refs(l.rows(m, m.inputs())), c.Row)

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

// filterDescOf names the filter key by the search standing in the list.
func filterDescOf(m *model) string {
	return m.filter.openDesc()
}

func filterClearDescOf(m *model) string {
	return m.filter.clearDesc()
}

func (m *model) activeListParams(in screen.Inputs) (list.Params, bool) {
	return m.listParamsOn(m, in)
}

func (m *model) listParamsOn(probe *model, in screen.Inputs) (list.Params, bool) {
	l, ok := m.active().(listRenderer)
	if !ok {
		return list.Params{}, false
	}

	return l.listParams(probe, in), true
}

// listHelp is a list screen's legend: its own rows, then the bindings every list shares.
func (m *model) listHelp(own legend.Help) legend.Help {
	// A disabled binding renders nothing but still takes a cell of the expanded legend.
	var tabs []key.Binding
	if b := m.tabsBinding(); b.Enabled() {
		tabs = []key.Binding{b}
	}
	if b := m.kindsBinding(); b.Enabled() {
		tabs = append(tabs, b)
	}

	return legend.Help{
		Short: slices.Concat(own.Short, m.globalHelp()),
		Full: slices.Concat(
			own.Full,
			tabs,
			m.fullGlobalHelp(),
		),
	}
}

// navPageStep is how many rows a page key moves the cursor.
const navPageStep = 10

// Lines is the room the list has once the chrome around it has taken its share.
func (m *model) Lines(chrome ...string) int {
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

func (m *model) listClicker(l keyedList) clicker {
	clicked := requestClick(func(m *model, key string, nth int) screen.Request { return l.click(m, m.inputs(), key, nth) })
	if !m.isListFocused(m.inputs()) || m.isViewsOpen() {
		return m.stopClicker(stopList, clicked)
	}

	return clickFunc(func(m *model, key string, nth int) tea.Cmd {
		if key != l.hovered(m, m.inputs()) {
			return clicked.click(m, key, nth)
		}

		band, ok := m.buildMenu()
		if !ok {
			return nil
		}

		return m.apply(menu.Open{Menu: band})
	})
}

func (m *model) isHoveredHit(hit zone.Hit) bool {
	lister, ok := m.active().(activeLister)

	return ok && hit.Zone == zoneList && hit.Key == lister.activeList().hovered(m, m.inputs())
}
