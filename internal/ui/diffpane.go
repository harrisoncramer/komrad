package ui

import (
	"slices"
	"strconv"

	"komrad/config"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
	"komrad/internal/vim"

	tea "charm.land/bubbletea/v2"
)

// diffPanStep is the smallest sideways jump, used until the pane has been measured.
const diffPanStep = 8

// diffWheelStep is how many rows one scroll key moves a diff.
const diffWheelStep = 3

// diffFloor is the narrowest a diff is worth rendering at: the gutter and enough of a line beside
// it to read.
const diffFloor = diffview.GutterWidth + 20

// diffKeys is the keys that walk a diff pane.
type diffKeys struct {
	keys     config.Keybindings
	register *vim.Register
	pane     *diffview.Pane
	filter   *filter.State
	// window is the size the panel renders the pane at.
	window func(screen.Inputs) diffview.Window
	// moved is what the screen does once the reader lands somewhere new, and is nil on a screen
	// with nothing to do.
	moved func() screen.Request
	// refuseExpand says why the file under the cursor may not be rendered whole, and is nil on a
	// screen that never refuses.
	refuseExpand func() string
}

func diffTable[S any](k diffKeys, in screen.Inputs) []keys.Action[S] {
	panning := func(S) bool { return !in.Room.Wrap }

	return append(keys.NavActions[S](keys.NavActionsParams{Keys: k.keys, Nav: k, In: in, Page: keys.NavPageStep, Scroll: diffWheelStep}), []keys.Action[S]{
		{
			ID:   actionDiffFile,
			Keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.NextFile, b.Diff.PrevFile) },
			Nav: func(_ S, pressed string) screen.Request {
				if !k.pane.JumpFile(keys.DirectionOf(pressed, k.keys.Diff.PrevFile), k.window(in)) {
					return nil
				}

				return k.land()
			},
		},
		{
			ID:   keys.FilterOpen,
			Keys: func(config.Keybindings) []string { return []string{filter.OpenKey} },
			Nav: func(S, string) screen.Request {
				k.filter.Open()
				k.search(in)

				return nil
			},
		},
		{
			ID:   actionDiffMatch,
			Keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.NextMatch, b.Diff.PrevMatch) },
			Nav: func(_ S, pressed string) screen.Request {
				return k.jumpMatch(in, keys.DirectionOf(pressed, k.keys.Diff.PrevMatch))
			},
		},
		{
			ID:   actionDiffPan,
			Keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.PanLeft, b.Diff.PanRight) },
			When: panning,
			Nav: func(_ S, pressed string) screen.Request {
				k.pane.PanBy(keys.DirectionOf(pressed, k.keys.Diff.PanLeft) * panStep(in))

				return nil
			},
		},
		{
			ID:   actionDiffPanReset,
			Keys: func(b config.Keybindings) []string { return b.Diff.PanReset },
			When: panning,
			Nav: func(S, string) screen.Request {
				k.pane.PanBy(-k.pane.Pan())

				return nil
			},
		},
	}...)
}

// diffShapeTable is the keys that expand and wrap a diff pane's files.
func diffShapeTable[S any](k diffKeys, in screen.Inputs) []keys.Action[S] {
	return []keys.Action[S]{
		{
			ID:   actionDiffExpand,
			Keys: func(b config.Keybindings) []string { return b.Diff.Expand },
			Nav:  func(S, string) screen.Request { return k.toggleExpanded(in) },
		},
		{
			ID:   actionDiffWrap,
			Keys: func(b config.Keybindings) []string { return b.Diff.Wrap },
			Nav:  func(S, string) screen.Request { return k.toggleWrap(in) },
		},
	}
}

// diffLegend is the diff's own rows of a screen's legend, in reading order.
func diffLegend[S any](k diffKeys, in screen.Inputs) []keys.LegendRow[S] {
	return []keys.LegendRow[S]{
		keys.MoveRow[S](),
		keys.NavPageRow[S](),
		diffFileRow[S](),
		diffSearchRow[S](),
		diffMatchRow[S](),
		diffPanRow[S](),
		keys.JumpRow[S](),
		{
			Of:   []keys.ID{actionDiffExpand},
			Desc: func(S) string { return k.expandDesc() },
		},
		diffWrapRow[S](in),
	}
}

func diffFileRow[S any]() keys.LegendRow[S] {
	return keys.LegendRow[S]{
		Of:   []keys.ID{actionDiffFile},
		Desc: func(S) string { return "next/previous file" },
		Label: func(k config.Keybindings) string {
			return keys.HeadLabel(k.Diff.NextFile) + "/" + keys.HeadLabel(k.Diff.PrevFile)
		},
	}
}

func diffSearchRow[S any]() keys.LegendRow[S] {
	return keys.LegendRow[S]{
		Of:   []keys.ID{keys.FilterOpen},
		Desc: func(S) string { return "search" },
	}
}

func diffMatchRow[S any]() keys.LegendRow[S] {
	return keys.LegendRow[S]{
		Of:   []keys.ID{actionDiffMatch},
		Desc: func(S) string { return "next/previous match" },
		Label: func(k config.Keybindings) string {
			return keys.HeadLabel(k.Diff.NextMatch) + "/" + keys.HeadLabel(k.Diff.PrevMatch)
		},
	}
}

// diffPanRow is panning and its reset, which share a row.
func diffPanRow[S any]() keys.LegendRow[S] {
	return keys.LegendRow[S]{
		Of:   []keys.ID{actionDiffPan, actionDiffPanReset},
		Desc: func(S) string { return "pan sideways" },
		Label: func(k config.Keybindings) string {
			return keys.HeadLabel(k.Diff.PanLeft) + "/" + keys.HeadLabel(k.Diff.PanRight) + "/" + keys.Label(k.Diff.PanReset)
		},
	}
}

// diffWrapRow is the key that wraps long lines or cuts them off again.
func diffWrapRow[S any](in screen.Inputs) keys.LegendRow[S] {
	return keys.LegendRow[S]{
		Of:   []keys.ID{actionDiffWrap},
		Desc: func(S) string { return wrapDesc(in) },
	}
}

func wrapDesc(in screen.Inputs) string {
	if in.Room.Wrap {
		return "stop wrapping lines"
	}

	return "wrap lines"
}

// expandDesc names what the expand key does to the file under the cursor.
func (k diffKeys) expandDesc() string {
	view := k.pane.View()
	if view == nil {
		return "show the whole file"
	}

	file, ok := view.GetRenderedFileAt(k.pane.Cursor())
	if ok && view.IsExpanded(file.Path()) {
		return "show the changes"
	}

	return "show the whole file"
}

// updateFilter is the search prompt taking keys.
func (k diffKeys) updateFilter(in screen.Inputs, key tea.KeyPressMsg) screen.Request {
	if key.String() == "ctrl+c" {
		return screen.Hide{}
	}

	if k.filter.TakeKey(key, k.register) {
		k.search(in)
	}

	return nil
}

// search runs the pattern over the diff and moves to the first thing it finds.
func (k diffKeys) search(in screen.Inputs) {
	if k.pane.View() == nil {
		return
	}

	k.pane.Search(k.filter.Matcher, k.window(in))
}

// jumpMatch steps to the next or previous line the search found, wrapping at the ends.
func (k diffKeys) jumpMatch(in screen.Inputs, direction int) screen.Request {
	moved, found := k.pane.JumpMatch(direction, k.window(in))
	if !found {
		said := "nothing to search for yet"
		if k.filter.IsStanding() {
			said = "no matches"
		}

		return screen.NewNote(screen.Warning, said)
	}

	if !moved {
		return nil
	}

	return k.land()
}

// toggleWrap wraps lines wider than the pane, or cuts them off again.
func (k diffKeys) toggleWrap(in screen.Inputs) screen.Request {
	in.Room.Wrap = !in.Room.Wrap
	if k.pane.View() != nil {
		k.pane.ScrollToCursor(k.window(in))
	}

	return screen.Wrap{On: in.Room.Wrap}
}

// toggleExpanded renders the whole of the file the cursor is in, or gives back the changed lines
// and the few around them.
func (k diffKeys) toggleExpanded(in screen.Inputs) screen.Request {
	if k.refuseExpand != nil {
		if refused := k.refuseExpand(); refused != "" {
			return screen.NewNote(screen.Warning, refused)
		}
	}

	var said screen.Request
	switch k.pane.ToggleExpanded(k.window(in)) {
	case diffview.ExpandNothing:
		return nil
	case diffview.ExpandNoMore:
		return screen.NewNote(screen.Warning, "there is no more of that file to show")
	case diffview.ExpandWhole:
		said = screen.NewNote(screen.Info, "showing the whole file")
	case diffview.ExpandChanged:
		said = screen.NewNote(screen.Info, "showing the changed lines")
	}

	return screen.Batch{said, k.land()}
}

func (k diffKeys) moveTo(in screen.Inputs, row int) screen.Request {
	if !k.pane.MoveTo(row, k.window(in)) {
		return nil
	}

	return k.land()
}

func (k diffKeys) step(in screen.Inputs, direction, rows int) screen.Request {
	if !k.pane.Step(direction, rows, k.window(in)) {
		return nil
	}

	return k.land()
}

// land tells the screen the reader is standing somewhere new.
func (k diffKeys) land() screen.Request {
	if k.moved == nil {
		return nil
	}

	return k.moved()
}

func (k diffKeys) MoveBy(in screen.Inputs, rows int) screen.Request {
	if rows < 0 {
		return k.step(in, -1, -rows)
	}

	return k.step(in, 1, rows)
}

func (k diffKeys) MoveToEnd(in screen.Inputs, direction int) screen.Request {
	if direction > 0 {
		return k.moveTo(in, k.pane.View().GetLastRow())
	}

	return k.moveTo(in, k.pane.View().GetFirstRow())
}

// panStep is a third of the pane, or the smallest jump until the pane is measured.
func panStep(in screen.Inputs) int {
	split := in.Room.Split
	if !split.On {
		return diffPanStep
	}

	return max(split.Column/3, diffPanStep)
}

func (k diffKeys) click(in screen.Inputs, key string) screen.Request {
	row, err := strconv.Atoi(key)
	if err != nil || k.pane.View() == nil {
		return nil
	}

	return k.moveTo(in, k.pane.View().Move(row-1, 1))
}
