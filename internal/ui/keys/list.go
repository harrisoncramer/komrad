package keys

import (
	"fmt"
	"slices"

	"komrad/config"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
)

// ScreenList is what the keys every list shares need of one screen: where its cursor and
// rows live, and what follows the cursor onto a row.
type ScreenList[T list.Keyed] struct {
	Keys   config.Keybindings
	Cursor func() *list.Cursor
	Rows   func(screen.Inputs) []T
	// Name is the text a search reads a row by, nil on a list that renders no filter.
	Name func(T) string
	// Fits is how a row meets a qualifier, nil on a list that reads none.
	Fits func(screen.Inputs) func(T) list.Fits
	// Refs is the rows as the selection sees them, nil on a list nothing can be selected in.
	Refs func([]T) []list.Ref
	// Header is what the toggle key names a header row as.
	Header string
	// Placed follows the cursor onto a row at once, when set.
	Placed func(screen.Inputs) screen.Request
	// Moved follows the cursor onto a row a key put it on, and is placed when nil.
	Moved func(screen.Inputs) screen.Request
}

// KeyedList is a list a search narrows and a click lands in.
type KeyedList interface {
	// Hovered is the key of the row the cursor is on, read before the pattern changes.
	Hovered(in screen.Inputs) string
	// Place puts the cursor back on that row, or as near to where it was as the narrowed list
	// allows.
	Place(in screen.Inputs, key string) screen.Request
	// FilterRows is every row on screen with the text a search reads, in the order they are
	// rendered.
	FilterRows(in screen.Inputs) []list.Row
	// Click puts the cursor on the nth row holding key.
	Click(in screen.Inputs, key string, nth int) screen.Request
}

// IsSelecting reports whether rows are selected.
func (l ScreenList[T]) IsSelecting() bool {
	return l.Cursor().Selected.IsActive()
}

// IsBrowsing reports whether no rows are selected.
func (l ScreenList[T]) IsBrowsing() bool {
	return !l.IsSelecting()
}

// Hovered is the key of the row the cursor is on.
func (l ScreenList[T]) Hovered(in screen.Inputs) string {
	return list.KeyAt(l.Rows(in), l.Cursor().Row)
}

// HoveredName is the name of the row the cursor is on.
func (l ScreenList[T]) HoveredName(in screen.Inputs) string {
	row, ok := list.At(l.Rows(in), l.Cursor().Row)
	if !ok || l.Name == nil {
		return ""
	}

	return l.Name(row)
}

// Place puts the cursor back on a row by key, or as near to where it was as the list allows.
func (l ScreenList[T]) Place(in screen.Inputs, hovered string) screen.Request {
	rows := l.Rows(in)
	c := l.Cursor()
	c.Row = cursor.Place(list.IndexOf(rows, hovered), c.Row, len(rows))
	if l.Placed != nil {
		return l.Placed(in)
	}

	return nil
}

// FilterRows is every row the search reads, by its key, name and fits.
func (l ScreenList[T]) FilterRows(in screen.Inputs) []list.Row {
	var fits func(T) list.Fits
	if l.Fits != nil {
		fits = l.Fits(in)
	}

	return xslice.Map(l.Rows(in), func(row T) list.Row {
		var rowFits list.Fits
		if fits != nil {
			rowFits = fits(row)
		}

		return list.Row{
			Key:  row.Key(),
			Name: l.Name(row),
			Fits: rowFits,
		}
	})
}

// Click puts the cursor on the nth row holding key.
func (l ScreenList[T]) Click(in screen.Inputs, key string, nth int) screen.Request {
	for i, row := range l.Rows(in) {
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

// MoveBy moves the cursor rows down, or up when negative.
func (l ScreenList[T]) MoveBy(in screen.Inputs, rows int) screen.Request {
	if rows == 1 || rows == -1 {
		return l.move(in, rows)
	}

	return l.jump(in, l.Cursor().Row+rows)
}

// MoveToEnd jumps the cursor to the last row, or the first when direction is negative.
func (l ScreenList[T]) MoveToEnd(in screen.Inputs, direction int) screen.Request {
	if direction > 0 {
		return l.jump(in, len(l.Rows(in))-1)
	}

	return l.jump(in, 0)
}

// ToggleDesc names what the select key does to the hovered row.
func (l ScreenList[T]) ToggleDesc(in screen.Inputs) string {
	c := l.Cursor()
	hovered, _ := list.At(l.Refs(l.Rows(in)), c.Row)

	switch {
	case hovered.IsHeader:
		return "select " + l.Header
	case c.Selected.Has(hovered.Key):
		return "deselect"
	}

	return "select"
}

// ListActions are the keys every list answers, in dispatch order.
func ListActions[S any, T list.Keyed](l ScreenList[T], in screen.Inputs) []Action[S] {
	var shared []Action[S]
	if l.Refs != nil {
		shared = append(shared, selectionActions[S](l, in)...)
	}
	shared = append(shared, Action[S]{
		ID:   Quit,
		Keys: FixedQuit,
		Nav:  func(S, string) screen.Request { return screen.Hide{} },
	})
	shared = append(shared, NavActions[S](NavActionsParams{
		Keys:   l.Keys,
		Nav:    l,
		In:     in,
		Page:   NavPageStep,
		Scroll: 1,
	})...)
	if l.Name != nil {
		shared = append(shared, filterActions[S](l, in)...)
	}

	return shared
}

// SelectionLegend is the three rows a standing selection is named by.
func SelectionLegend[S any, T list.Keyed](l ScreenList[T], in screen.Inputs) []LegendRow[S] {
	selecting := func(S) bool { return l.IsSelecting() }

	return []LegendRow[S]{
		{
			ActionIDs:     []ID{SelectToggle},
			Desc:          func(S) string { return l.ToggleDesc(in) },
			Show:          selecting,
			IsInShortHelp: true,
		},
		{
			ActionIDs: []ID{selectRange},
			Desc:      func(S) string { return "select to nearest" },
			Show:      selecting,
		},
		{
			ActionIDs:     []ID{selectClear},
			Desc:          func(S) string { return l.clearDesc() },
			Show:          selecting,
			IsInShortHelp: true,
		},
	}
}

// FilterDescOf names the filter key by the search standing in the list.
func FilterDescOf[S any](in screen.Inputs) func(S) string {
	return func(S) string { return filter.PatternMode(in.Room.Pattern).OpenDesc(in.Room.Pattern.Query) }
}

// FilterClearDescOf names the key that clears the search standing.
func FilterClearDescOf[S any](in screen.Inputs) func(S) string {
	return func(S) string { return filter.PatternMode(in.Room.Pattern).ClearDesc() }
}

func (l ScreenList[T]) landed(in screen.Inputs) screen.Request {
	if l.Moved != nil {
		return l.Moved(in)
	}

	if l.Placed != nil {
		return l.Placed(in)
	}

	return nil
}

func (l ScreenList[T]) move(in screen.Inputs, delta int) screen.Request {
	next, ok := l.Cursor().Move(delta, len(l.Rows(in)))
	if !ok {
		return nil
	}

	*l.Cursor() = next

	return l.landed(in)
}

func (l ScreenList[T]) jump(in screen.Inputs, idx int) screen.Request {
	next, ok := l.Cursor().Jump(idx, len(l.Rows(in)))
	if !ok {
		return nil
	}

	*l.Cursor() = next

	return l.landed(in)
}

func selectionActions[S any, T list.Keyed](l ScreenList[T], in screen.Inputs) []Action[S] {
	return []Action[S]{
		{
			ID:   selectClear,
			Keys: func(k config.Keybindings) []string { return k.Selection.Clear },
			When: func(S) bool { return l.IsSelecting() },
			Nav:  func(S, string) screen.Request { return l.clearSelection() },
		},
		{
			ID:   SelectToggle,
			Keys: func(k config.Keybindings) []string { return k.Selection.Toggle },
			Nav:  func(S, string) screen.Request { return l.toggleSelection(in) },
		},
		{
			ID:   selectRange,
			Keys: func(k config.Keybindings) []string { return k.Selection.Range },
			Nav:  func(S, string) screen.Request { return l.extendSelection(in) },
		},
	}
}

func filterActions[S any, T list.Keyed](l ScreenList[T], in screen.Inputs) []Action[S] {
	browsing := func(S) bool { return l.IsBrowsing() }
	standing := func(S) bool { return l.IsBrowsing() && in.Room.Pattern.IsStanding() }

	return []Action[S]{
		{
			ID:   FilterOpen,
			Keys: func(config.Keybindings) []string { return []string{filter.OpenKey} },
			When: browsing,
			Nav:  func(S, string) screen.Request { return screen.BeginFilter{} },
		},
		{
			ID:   FilterMatch,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
			When: standing,
			Nav: func(_ S, pressed string) screen.Request {
				return screen.MoveToMatch{Direction: DirectionOf(pressed, l.Keys.Filter.PrevMatch)}
			},
		},
		{
			ID:   FilterClear,
			Keys: FixedBack,
			When: standing,
			Nav: func(S, string) screen.Request {
				return screen.Batch{screen.ClearFilterRestoreCursor{}, screen.WaitForCursorRest{}}
			},
		},
	}
}

func (l ScreenList[T]) clearSelection() screen.Request {
	l.Cursor().Selected = list.Selection{}

	return nil
}

func (l ScreenList[T]) toggleSelection(in screen.Inputs) screen.Request {
	c := l.Cursor()
	c.Selected = c.Selected.Toggle(l.Refs(l.Rows(in)), c.Row)

	return nil
}

func (l ScreenList[T]) extendSelection(in screen.Inputs) screen.Request {
	if !l.IsSelecting() {
		return l.toggleSelection(in)
	}

	c := l.Cursor()
	c.Selected = c.Selected.Extend(l.Refs(l.Rows(in)), c.Row)

	return nil
}

func (l ScreenList[T]) clearDesc() string {
	return fmt.Sprintf("clear %d selected", len(l.Cursor().Selected.Keys))
}
