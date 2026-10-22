package ui

import (
	"slices"
	"strconv"

	"komrad/config"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"

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
	pane   *diffview.Pane
	filter *filterState
	// window is the size the panel renders the pane at.
	window func(*model) diffview.Window
	// moved is what the screen does once the reader lands somewhere new, and is nil on a screen
	// with nothing to do.
	moved func(*model) screen.Request
	// refuseExpand says why the file under the cursor may not be rendered whole, and is nil on a
	// screen that never refuses.
	refuseExpand func(*model) string
}

func (k diffKeys) table() []keys.Action[*model] {
	return append(navActions(k, navSteps{page: navPageStep, scroll: diffWheelStep}), []keys.Action[*model]{
		{
			ID:   actionDiffFile,
			Keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.NextFile, b.Diff.PrevFile) },
			Nav: func(m *model, pressed string) screen.Request {
				if !k.pane.JumpFile(keys.DirectionOf(pressed, m.keys.Diff.PrevFile), k.window(m)) {
					return nil
				}

				return k.land(m)
			},
		},
		{
			ID:   keys.FilterOpen,
			Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
			Nav: func(m *model, _ string) screen.Request {
				k.filter.open()
				k.search(m)

				return nil
			},
		},
		{
			ID:   actionDiffMatch,
			Keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.NextMatch, b.Diff.PrevMatch) },
			Nav: func(m *model, pressed string) screen.Request {
				return k.jumpMatch(m, keys.DirectionOf(pressed, m.keys.Diff.PrevMatch))
			},
		},
		{
			ID:   actionDiffPan,
			Keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.PanLeft, b.Diff.PanRight) },
			When: isPanning,
			Nav: func(m *model, pressed string) screen.Request {
				k.pane.PanBy(keys.DirectionOf(pressed, m.keys.Diff.PanLeft) * panStep(m))

				return nil
			},
		},
		{
			ID:   actionDiffPanReset,
			Keys: func(b config.Keybindings) []string { return b.Diff.PanReset },
			When: isPanning,
			Nav: func(*model, string) screen.Request {
				k.pane.PanBy(-k.pane.Pan())

				return nil
			},
		},
	}...)
}

// shapeTable is the keys that expand and wrap a diff pane's files.
func (k diffKeys) shapeTable() []keys.Action[*model] {
	return []keys.Action[*model]{
		{
			ID:   actionDiffExpand,
			Keys: func(b config.Keybindings) []string { return b.Diff.Expand },
			Nav:  func(m *model, _ string) screen.Request { return k.toggleExpanded(m) },
		},
		{
			ID:   actionDiffWrap,
			Keys: func(b config.Keybindings) []string { return b.Diff.Wrap },
			Nav:  func(m *model, _ string) screen.Request { return k.toggleWrap(m) },
		},
	}
}

// legend is the diff's own rows of a screen's legend, in reading order.
func (k diffKeys) legend() []keys.LegendRow[*model] {
	return []keys.LegendRow[*model]{
		keys.MoveRow[*model](),
		keys.NavPageRow[*model](),
		diffFileRow(),
		diffSearchRow(),
		diffMatchRow(),
		diffPanRow(),
		keys.JumpRow[*model](),
		{
			Of:   []keys.ID{actionDiffExpand},
			Desc: func(*model) string { return k.expandDesc() },
		},
		diffWrapRow(),
	}
}

func diffFileRow() keys.LegendRow[*model] {
	return keys.LegendRow[*model]{
		Of:   []keys.ID{actionDiffFile},
		Desc: func(*model) string { return "next/previous file" },
		Label: func(k config.Keybindings) string {
			return keys.HeadLabel(k.Diff.NextFile) + "/" + keys.HeadLabel(k.Diff.PrevFile)
		},
	}
}

func diffSearchRow() keys.LegendRow[*model] {
	return keys.LegendRow[*model]{
		Of:   []keys.ID{keys.FilterOpen},
		Desc: func(*model) string { return "search" },
	}
}

func diffMatchRow() keys.LegendRow[*model] {
	return keys.LegendRow[*model]{
		Of:   []keys.ID{actionDiffMatch},
		Desc: func(*model) string { return "next/previous match" },
		Label: func(k config.Keybindings) string {
			return keys.HeadLabel(k.Diff.NextMatch) + "/" + keys.HeadLabel(k.Diff.PrevMatch)
		},
	}
}

// diffPanRow is panning and its reset, which share a row.
func diffPanRow() keys.LegendRow[*model] {
	return keys.LegendRow[*model]{
		Of:   []keys.ID{actionDiffPan, actionDiffPanReset},
		Desc: func(*model) string { return "pan sideways" },
		Label: func(k config.Keybindings) string {
			return keys.HeadLabel(k.Diff.PanLeft) + "/" + keys.HeadLabel(k.Diff.PanRight) + "/" + keys.Label(k.Diff.PanReset)
		},
	}
}

// diffWrapRow is the key that wraps long lines or cuts them off again.
func diffWrapRow() keys.LegendRow[*model] {
	return keys.LegendRow[*model]{
		Of:   []keys.ID{actionDiffWrap},
		Desc: wrapDesc,
	}
}

func wrapDesc(m *model) string {
	if m.diffWrap {
		return "stop wrapping lines"
	}

	return "wrap lines"
}

// isPanning reports whether long lines are cut off and panned rather than wrapped.
func isPanning(m *model) bool {
	return !m.diffWrap
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
func (k diffKeys) updateFilter(m *model, key tea.KeyPressMsg) screen.Request {
	if key.String() == "ctrl+c" {
		return screen.Hide{}
	}

	if k.filter.takeKey(key, m.register) {
		k.search(m)
	}

	return nil
}

// search runs the pattern over the diff and moves to the first thing it finds.
func (k diffKeys) search(m *model) {
	if k.pane.View() == nil {
		return
	}

	k.pane.Search(k.filter.pattern, k.window(m))
}

// jumpMatch steps to the next or previous line the search found, wrapping at the ends.
func (k diffKeys) jumpMatch(m *model, direction int) screen.Request {
	moved, found := k.pane.JumpMatch(direction, k.window(m))
	if !found {
		said := "nothing to search for yet"
		if k.filter.isStanding() {
			said = "no matches"
		}

		return screen.NewNote(screen.Warning, said)
	}

	if !moved {
		return nil
	}

	return k.land(m)
}

// toggleWrap wraps lines wider than the pane, or cuts them off again.
func (k diffKeys) toggleWrap(m *model) screen.Request {
	m.diffWrap = !m.diffWrap
	if k.pane.View() != nil {
		k.pane.ScrollToCursor(k.window(m))
	}

	return nil
}

// toggleExpanded renders the whole of the file the cursor is in, or gives back the changed lines
// and the few around them.
func (k diffKeys) toggleExpanded(m *model) screen.Request {
	if k.refuseExpand != nil {
		if refused := k.refuseExpand(m); refused != "" {
			return screen.NewNote(screen.Warning, refused)
		}
	}

	switch k.pane.ToggleExpanded(k.window(m)) {
	case diffview.ExpandNothing:
		return nil
	case diffview.ExpandNoMore:
		return screen.NewNote(screen.Warning, "there is no more of that file to show")
	case diffview.ExpandWhole:
		return screen.Batch{screen.NewNote(screen.Info, "showing the whole file"), k.land(m)}
	case diffview.ExpandChanged:
		return screen.Batch{screen.NewNote(screen.Info, "showing the changed lines"), k.land(m)}
	}

	return k.land(m)
}

func (k diffKeys) moveTo(m *model, row int) screen.Request {
	if !k.pane.MoveTo(row, k.window(m)) {
		return nil
	}

	return k.land(m)
}

func (k diffKeys) step(m *model, direction, rows int) screen.Request {
	if !k.pane.Step(direction, rows, k.window(m)) {
		return nil
	}

	return k.land(m)
}

// land tells the screen the reader is standing somewhere new.
func (k diffKeys) land(m *model) screen.Request {
	if k.moved == nil {
		return nil
	}

	return k.moved(m)
}

func (k diffKeys) moveBy(m *model, rows int) screen.Request {
	if rows < 0 {
		return k.step(m, -1, -rows)
	}

	return k.step(m, 1, rows)
}

func (k diffKeys) moveToEnd(m *model, direction int) screen.Request {
	if direction > 0 {
		return k.moveTo(m, k.pane.View().GetLastRow())
	}

	return k.moveTo(m, k.pane.View().GetFirstRow())
}

// panStep is a third of the pane, or the smallest jump until the pane is measured.
func panStep(m *model) int {
	split, ok := m.screenColumnSplit()
	if !ok {
		return diffPanStep
	}

	return max(split.column/3, diffPanStep)
}

func (k diffKeys) click(m *model, key string) screen.Request {
	row, err := strconv.Atoi(key)
	if err != nil || k.pane.View() == nil {
		return nil
	}

	return k.moveTo(m, k.pane.View().Move(row-1, 1))
}
