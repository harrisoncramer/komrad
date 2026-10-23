package keys

import (
	"slices"
	"strconv"

	"komrad/config"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/screen"
	"komrad/internal/vim"

	tea "charm.land/bubbletea/v2"
)

// diffPanStep is the smallest sideways jump, used until the pane has been measured.
const diffPanStep = 8

// DiffWheelStep is how many rows one scroll key moves a diff.
const DiffWheelStep = 3

// Diff is the keys that walk a diff pane.
type Diff struct {
	Keys     config.Keybindings
	Register *vim.Register
	Pane     *diffview.Pane
	Filter   *filter.State
	// Window is the size the panel renders the pane at.
	Window func(screen.Inputs) diffview.Window
	// Moved is what the screen does once the reader lands somewhere new, and is nil on a screen
	// with nothing to do.
	Moved func() screen.Request
	// RefuseExpand says why the file under the cursor may not be rendered whole, and is nil on a
	// screen that never refuses.
	RefuseExpand func() string
}

// DiffTable is the keys that move through a diff pane, search it and pan it.
func DiffTable[S any](k Diff, in screen.Inputs) []Action[S] {
	panning := func(S) bool { return !in.Room.IsDiffWrapped }

	return append(NavActions[S](NavActionsParams{
		Keys:   k.Keys,
		Nav:    k,
		In:     in,
		Page:   NavPageStep,
		Scroll: DiffWheelStep,
	}), []Action[S]{
		{
			ID:   DiffFile,
			Keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.NextFile, b.Diff.PrevFile) },
			Nav: func(_ S, pressed string) screen.Request {
				if !k.Pane.JumpFile(DirectionOf(pressed, k.Keys.Diff.PrevFile), k.Window(in)) {
					return nil
				}

				return k.Land()
			},
		},
		{
			ID:   FilterOpen,
			Keys: func(config.Keybindings) []string { return []string{filter.OpenKey} },
			Nav: func(S, string) screen.Request {
				k.Filter.Open()
				k.search(in)

				return nil
			},
		},
		{
			ID:   diffMatch,
			Keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.NextMatch, b.Diff.PrevMatch) },
			Nav: func(_ S, pressed string) screen.Request {
				return k.jumpMatch(in, DirectionOf(pressed, k.Keys.Diff.PrevMatch))
			},
		},
		{
			ID:   diffPan,
			Keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.PanLeft, b.Diff.PanRight) },
			When: panning,
			Nav: func(_ S, pressed string) screen.Request {
				k.Pane.PanBy(DirectionOf(pressed, k.Keys.Diff.PanLeft) * panStep(in))

				return nil
			},
		},
		{
			ID:   diffPanReset,
			Keys: func(b config.Keybindings) []string { return b.Diff.PanReset },
			When: panning,
			Nav: func(S, string) screen.Request {
				k.Pane.PanBy(-k.Pane.Pan())

				return nil
			},
		},
	}...)
}

// DiffShapeTable is the keys that expand and wrap a diff pane's files.
func DiffShapeTable[S any](k Diff, in screen.Inputs) []Action[S] {
	return []Action[S]{
		{
			ID:   diffExpand,
			Keys: func(b config.Keybindings) []string { return b.Diff.Expand },
			Nav:  func(S, string) screen.Request { return k.ToggleExpanded(in) },
		},
		{
			ID:   diffWrap,
			Keys: func(b config.Keybindings) []string { return b.Diff.Wrap },
			Nav:  func(S, string) screen.Request { return k.ToggleWrap(in) },
		},
	}
}

// DiffLegend is the diff's own rows of a screen's legend, in reading order.
func DiffLegend[S any](k Diff, in screen.Inputs) []LegendRow[S] {
	return []LegendRow[S]{
		MoveRow[S](),
		NavPageRow[S](),
		DiffFileRow[S](),
		DiffSearchRow[S](),
		DiffMatchRow[S](),
		DiffPanRow[S](),
		JumpRow[S](),
		{
			ActionIDs: []ID{diffExpand},
			Desc:      func(S) string { return k.ExpandDesc() },
		},
		diffWrapRow[S](in),
	}
}

// DiffFileRow names the keys that step between a diff's files.
func DiffFileRow[S any]() LegendRow[S] {
	return LegendRow[S]{
		ActionIDs: []ID{DiffFile},
		Desc:      func(S) string { return "next/previous file" },
		Label: func(k config.Keybindings) string {
			return HeadLabel(k.Diff.NextFile) + "/" + HeadLabel(k.Diff.PrevFile)
		},
	}
}

// DiffSearchRow names the key that opens a diff's search.
func DiffSearchRow[S any]() LegendRow[S] {
	return LegendRow[S]{
		ActionIDs: []ID{FilterOpen},
		Desc:      func(S) string { return "search" },
	}
}

// DiffMatchRow names the keys that step between a diff's matches.
func DiffMatchRow[S any]() LegendRow[S] {
	return LegendRow[S]{
		ActionIDs: []ID{diffMatch},
		Desc:      func(S) string { return "next/previous match" },
		Label: func(k config.Keybindings) string {
			return HeadLabel(k.Diff.NextMatch) + "/" + HeadLabel(k.Diff.PrevMatch)
		},
	}
}

// DiffPanRow is panning and its reset, which share a row.
func DiffPanRow[S any]() LegendRow[S] {
	return LegendRow[S]{
		ActionIDs: []ID{diffPan, diffPanReset},
		Desc:      func(S) string { return "pan sideways" },
		Label: func(k config.Keybindings) string {
			return HeadLabel(k.Diff.PanLeft) + "/" + HeadLabel(k.Diff.PanRight) + "/" + Label(k.Diff.PanReset)
		},
	}
}

// diffWrapRow is the key that wraps long lines or cuts them off again.
func diffWrapRow[S any](in screen.Inputs) LegendRow[S] {
	return LegendRow[S]{
		ActionIDs: []ID{diffWrap},
		Desc:      func(S) string { return WrapDesc(in) },
	}
}

// WrapDesc names what the wrap key does to the diff's long lines.
func WrapDesc(in screen.Inputs) string {
	if in.Room.IsDiffWrapped {
		return "stop wrapping lines"
	}

	return "wrap lines"
}

// ExpandDesc names what the expand key does to the file under the cursor.
func (k Diff) ExpandDesc() string {
	view := k.Pane.View()
	if view == nil {
		return "show the whole file"
	}

	file, ok := view.GetRenderedFileAt(k.Pane.Cursor())
	if ok && view.IsExpanded(file.Path()) {
		return "show the changes"
	}

	return "show the whole file"
}

// UpdateFilter is the search prompt taking keys.
func (k Diff) UpdateFilter(in screen.Inputs, key tea.KeyPressMsg) screen.Request {
	if key.String() == "ctrl+c" {
		return screen.Hide{}
	}

	if k.Filter.TakeKey(key, k.Register) {
		k.search(in)
	}

	return nil
}

// search runs the pattern over the diff and moves to the first thing it finds.
func (k Diff) search(in screen.Inputs) {
	if k.Pane.View() == nil {
		return
	}

	k.Pane.Search(k.Filter.Matcher, k.Window(in))
}

// jumpMatch steps to the next or previous line the search found, wrapping at the ends.
func (k Diff) jumpMatch(in screen.Inputs, direction int) screen.Request {
	moved, found := k.Pane.JumpMatch(direction, k.Window(in))
	if !found {
		said := "nothing to search for yet"
		if k.Filter.IsStanding() {
			said = "no matches"
		}

		return screen.NewPostMessage(screen.Warning, said)
	}

	if !moved {
		return nil
	}

	return k.Land()
}

// ToggleWrap wraps lines wider than the pane, or cuts them off again.
func (k Diff) ToggleWrap(in screen.Inputs) screen.Request {
	in.Room.IsDiffWrapped = !in.Room.IsDiffWrapped
	if k.Pane.View() != nil {
		k.Pane.ScrollToCursor(k.Window(in))
	}

	return screen.SetDiffWrap{ShouldWrap: in.Room.IsDiffWrapped}
}

// ToggleExpanded renders the whole of the file the cursor is in, or gives back the changed lines
// and the few around them.
func (k Diff) ToggleExpanded(in screen.Inputs) screen.Request {
	if k.RefuseExpand != nil {
		if refused := k.RefuseExpand(); refused != "" {
			return screen.NewPostMessage(screen.Warning, refused)
		}
	}

	var said screen.Request
	switch k.Pane.ToggleExpanded(k.Window(in)) {
	case diffview.ExpandNothing:
		return nil
	case diffview.ExpandNoMore:
		return screen.NewPostMessage(screen.Warning, "there is no more of that file to show")
	case diffview.ExpandWhole:
		said = screen.NewPostMessage(screen.Info, "showing the whole file")
	case diffview.ExpandChanged:
		said = screen.NewPostMessage(screen.Info, "showing the changed lines")
	}

	return screen.Batch{said, k.Land()}
}

// MoveTo puts the reader on a row of the diff.
func (k Diff) MoveTo(in screen.Inputs, row int) screen.Request {
	if !k.Pane.MoveTo(row, k.Window(in)) {
		return nil
	}

	return k.Land()
}

func (k Diff) step(in screen.Inputs, direction, rows int) screen.Request {
	if !k.Pane.Step(direction, rows, k.Window(in)) {
		return nil
	}

	return k.Land()
}

// Land tells the screen the reader is standing somewhere new.
func (k Diff) Land() screen.Request {
	if k.Moved == nil {
		return nil
	}

	return k.Moved()
}

// MoveBy walks the reader down the diff by rows, up it when rows is negative.
func (k Diff) MoveBy(in screen.Inputs, rows int) screen.Request {
	if rows < 0 {
		return k.step(in, -1, -rows)
	}

	return k.step(in, 1, rows)
}

// MoveToEnd puts the reader on the diff's last row for a positive direction and its first otherwise.
func (k Diff) MoveToEnd(in screen.Inputs, direction int) screen.Request {
	if direction > 0 {
		return k.MoveTo(in, k.Pane.View().GetLastRow())
	}

	return k.MoveTo(in, k.Pane.View().GetFirstRow())
}

// panStep is a third of the pane, or the smallest jump until the pane is measured.
func panStep(in screen.Inputs) int {
	split := in.Room.OwnBlockSplit
	if !split.IsDivided {
		return diffPanStep
	}

	return max(split.ColumnWidth/3, diffPanStep)
}

// Click puts the reader on the row a click marked with key.
func (k Diff) Click(in screen.Inputs, key string) screen.Request {
	row, err := strconv.Atoi(key)
	if err != nil || k.Pane.View() == nil {
		return nil
	}

	return k.MoveTo(in, k.Pane.View().Move(row-1, 1))
}
