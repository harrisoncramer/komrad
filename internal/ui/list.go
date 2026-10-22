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
	rows   func(screen.Inputs) []T
	// name is the text a search reads a row by, nil on a list that renders no filter.
	name func(T) string
	// fits is how a row meets a qualifier, nil on a list that reads none.
	fits func(screen.Inputs) func(T) list.Fits
	// refs is the rows as the selection sees them, nil on a list nothing can be selected in.
	refs func([]T) []list.Ref
	// header is what the toggle key names a header row as.
	header string
	// placed follows the cursor onto a row at once, when set.
	placed func(screen.Inputs) screen.Request
	// moved follows the cursor onto a row a key put it on, and is placed when nil.
	moved func(screen.Inputs) screen.Request
}

func (l screenList[T]) isSelecting() bool {
	return l.cursor().Selected.IsActive()
}

func (l screenList[T]) isBrowsing() bool {
	return !l.isSelecting()
}

// Hovered is the key of the row the cursor is on.
func (l screenList[T]) Hovered(in screen.Inputs) string {
	return list.KeyAt(l.rows(in), l.cursor().Row)
}

func (l screenList[T]) hoveredName(in screen.Inputs) string {
	row, ok := list.At(l.rows(in), l.cursor().Row)
	if !ok || l.name == nil {
		return ""
	}

	return l.name(row)
}

// Place puts the cursor back on a row by key, or as near to where it was as the list allows.
func (l screenList[T]) Place(in screen.Inputs, hovered string) screen.Request {
	rows := l.rows(in)
	c := l.cursor()
	c.Row = cursor.Place(list.IndexOf(rows, hovered), c.Row, len(rows))
	if l.placed != nil {
		return l.placed(in)
	}

	return nil
}

func (l screenList[T]) FilterRows(in screen.Inputs) []list.Row {
	var fits func(T) list.Fits
	if l.fits != nil {
		fits = l.fits(in)
	}

	return xslice.Map(l.rows(in), func(row T) list.Row {
		var rowFits list.Fits
		if fits != nil {
			rowFits = fits(row)
		}

		return list.Row{
			Key:  row.Key(),
			Name: l.name(row),
			Fits: rowFits,
		}
	})
}

func (l screenList[T]) landed(in screen.Inputs) screen.Request {
	if l.moved != nil {
		return l.moved(in)
	}

	if l.placed != nil {
		return l.placed(in)
	}

	return nil
}

func (l screenList[T]) move(in screen.Inputs, delta int) screen.Request {
	next, ok := l.cursor().Move(delta, len(l.rows(in)))
	if !ok {
		return nil
	}

	*l.cursor() = next

	return l.landed(in)
}

func (l screenList[T]) jump(in screen.Inputs, idx int) screen.Request {
	next, ok := l.cursor().Jump(idx, len(l.rows(in)))
	if !ok {
		return nil
	}

	*l.cursor() = next

	return l.landed(in)
}

func (l screenList[T]) Click(in screen.Inputs, key string, nth int) screen.Request {
	for i, row := range l.rows(in) {
		if row.Key() != key {
			continue
		}

		if nth > 0 {
			nth--

			continue
		}

		return l.jump(in, i)
	}

	return nil
}

// listActions are the keys every list answers, in dispatch order.
func listActions[S any, T list.Keyed](l screenList[T], in screen.Inputs) []keys.Action[S] {
	var shared []keys.Action[S]
	if l.refs != nil {
		shared = append(shared, selectionActions[S](l, in)...)
	}
	shared = append(shared, keys.Action[S]{
		ID:   keys.Quit,
		Keys: keys.FixedQuit,
		Nav:  func(S, string) screen.Request { return screen.Hide{} },
	})
	shared = append(shared, navActions[S](navActionsParams{keys: l.keys, nav: l, in: in, page: navPageStep, scroll: 1})...)
	if l.name != nil {
		shared = append(shared, filterActions[S](l, in)...)
	}

	return shared
}

func selectionActions[S any, T list.Keyed](l screenList[T], in screen.Inputs) []keys.Action[S] {
	return []keys.Action[S]{
		{
			ID:   keys.SelectClear,
			Keys: func(k config.Keybindings) []string { return k.Selection.Clear },
			When: func(S) bool { return l.isSelecting() },
			Nav:  func(S, string) screen.Request { return l.clearSelection() },
		},
		{
			ID:   keys.SelectToggle,
			Keys: func(k config.Keybindings) []string { return k.Selection.Toggle },
			Nav:  func(S, string) screen.Request { return l.toggleSelection(in) },
		},
		{
			ID:   keys.SelectRange,
			Keys: func(k config.Keybindings) []string { return k.Selection.Range },
			Nav:  func(S, string) screen.Request { return l.extendSelection(in) },
		},
	}
}

func filterActions[S any, T list.Keyed](l screenList[T], in screen.Inputs) []keys.Action[S] {
	browsing := func(S) bool { return l.isBrowsing() }
	standing := func(S) bool { return l.isBrowsing() && in.Room.Pattern.IsStanding() }

	return []keys.Action[S]{
		{
			ID:   keys.FilterOpen,
			Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
			When: browsing,
			Nav:  func(S, string) screen.Request { return screen.BeginFilter{} },
		},
		{
			ID:   keys.FilterMatch,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
			When: standing,
			Nav: func(_ S, pressed string) screen.Request {
				return screen.StepMatch{Delta: keys.DirectionOf(pressed, l.keys.Filter.PrevMatch)}
			},
		},
		{
			ID:   keys.FilterClear,
			Keys: keys.FixedBack,
			When: standing,
			Nav:  func(S, string) screen.Request { return screen.Batch{screen.ClearFilter{}, screen.Settle{}} },
		},
	}
}

func (l screenList[T]) moveBy(in screen.Inputs, rows int) screen.Request {
	if rows == 1 || rows == -1 {
		return l.move(in, rows)
	}

	return l.jump(in, l.cursor().Row+rows)
}

func (l screenList[T]) moveToEnd(in screen.Inputs, direction int) screen.Request {
	if direction > 0 {
		return l.jump(in, len(l.rows(in))-1)
	}

	return l.jump(in, 0)
}

func (l screenList[T]) clearSelection() screen.Request {
	l.cursor().Selected = list.Selection{}

	return nil
}

func (l screenList[T]) toggleSelection(in screen.Inputs) screen.Request {
	c := l.cursor()
	c.Selected = c.Selected.Toggle(l.refs(l.rows(in)), c.Row)

	return nil
}

func (l screenList[T]) extendSelection(in screen.Inputs) screen.Request {
	if !l.isSelecting() {
		return l.toggleSelection(in)
	}

	c := l.cursor()
	c.Selected = c.Selected.Extend(l.refs(l.rows(in)), c.Row)

	return nil
}

// selectionLegend is the three rows a standing selection is named by.
func selectionLegend[S any, T list.Keyed](l screenList[T], in screen.Inputs) []keys.LegendRow[S] {
	selecting := func(S) bool { return l.isSelecting() }

	return []keys.LegendRow[S]{
		{
			Of:    []keys.ID{keys.SelectToggle},
			Desc:  func(S) string { return l.toggleDesc(in) },
			Show:  selecting,
			Strip: true,
		},
		{
			Of:   []keys.ID{keys.SelectRange},
			Desc: func(S) string { return "select to nearest" },
			Show: selecting,
		},
		{
			Of:    []keys.ID{keys.SelectClear},
			Desc:  func(S) string { return l.clearDesc() },
			Show:  selecting,
			Strip: true,
		},
	}
}

func (l screenList[T]) toggleDesc(in screen.Inputs) string {
	c := l.cursor()
	hovered, _ := list.At(l.refs(l.rows(in)), c.Row)

	switch {
	case hovered.Header:
		return "select " + l.header
	case c.Selected.Has(hovered.Key):
		return "deselect"
	}

	return "select"
}

func (l screenList[T]) clearDesc() string {
	return fmt.Sprintf("clear %d selected", len(l.cursor().Selected.Keys))
}

// filterDescOf names the filter key by the search standing in the list.
func filterDescOf[S any](in screen.Inputs) func(S) string {
	return func(S) string { return patternMode(in.Room.Pattern).openDesc(in.Room.Pattern.Typed) }
}

func filterClearDescOf[S any](in screen.Inputs) func(S) string {
	return func(S) string { return patternMode(in.Room.Pattern).clearDesc() }
}

func (m *model) activeListParams(in screen.Inputs) (list.Params, bool) {
	l, ok := m.active().(listRenderer)
	if !ok {
		return list.Params{}, false
	}

	return l.ListParams(in), true
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
	clicked := requestClick(func(m *model, key string, nth int) screen.Request { return l.Click(m.inputs(), key, nth) })
	if !m.isListFocused(m.inputs()) || m.isViewsOpen() {
		return m.stopClicker(stopList, clicked)
	}

	return clickFunc(func(m *model, key string, nth int) tea.Cmd {
		if key != l.Hovered(m.inputs()) {
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

	return ok && hit.Zone == zoneList && hit.Key == lister.ActiveList().Hovered(m.inputs())
}
