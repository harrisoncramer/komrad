package ui

import (
	"fmt"
	"slices"

	"komrad/config"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint/label"

	tea "charm.land/bubbletea/v2"
)

// diffPanStep is the smallest sideways jump, used until the pane has been measured.
const diffPanStep = 8

// diffWheelStep is how many rows one scroll key moves a diff.
const diffWheelStep = 3

// diffFloor is the narrowest a diff is worth drawing at: the gutter and enough of a line beside
// it to read.
const diffFloor = diffview.GutterWidth + 20

// diffKeys is the keys that walk a diff pane.
type diffKeys struct {
	pane   *diffview.Pane
	filter *filterState
	// window is the size the pane is drawn at.
	window func(*model) diffview.Window
	// moved is what the screen does once the reader lands somewhere new, and is nil on a screen
	// with nothing to do.
	moved func(*model) tea.Cmd
	// refuseExpand says why the file under the cursor may not be drawn whole, and is nil on a
	// screen that never refuses.
	refuseExpand func(*model) string
}

func (k diffKeys) table() []screenAction {
	return append(navActions(k, navSteps{page: navPageStep, scroll: diffWheelStep}), []screenAction{
		{
			id:   actionDiffFile,
			keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.NextFile, b.Diff.PrevFile) },
			nav: func(m *model, pressed string) tea.Cmd {
				if !k.pane.JumpFile(directionOf(pressed, m.keys.Diff.PrevFile), k.window(m)) {
					return nil
				}

				return k.land(m)
			},
		},
		{
			id:   actionFilterOpen,
			keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
			nav: func(m *model, _ string) tea.Cmd {
				k.filter.open()
				k.search(m)

				return nil
			},
		},
		{
			id:   actionDiffMatch,
			keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.NextMatch, b.Diff.PrevMatch) },
			nav: func(m *model, pressed string) tea.Cmd {
				return k.jumpMatch(m, directionOf(pressed, m.keys.Diff.PrevMatch))
			},
		},
		{
			id:   actionDiffPan,
			keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.PanLeft, b.Diff.PanRight) },
			when: isPanning,
			nav: func(m *model, pressed string) tea.Cmd {
				k.pane.PanBy(directionOf(pressed, m.keys.Diff.PanLeft) * panStep(m))

				return nil
			},
		},
		{
			id:   actionDiffPanReset,
			keys: func(b config.Keybindings) []string { return b.Diff.PanReset },
			when: isPanning,
			nav: func(*model, string) tea.Cmd {
				k.pane.PanBy(-k.pane.Pan())

				return nil
			},
		},
	}...)
}

// shapeTable is the keys that expand and wrap a diff pane's files.
func (k diffKeys) shapeTable() []screenAction {
	return []screenAction{
		{
			id:   actionDiffExpand,
			keys: func(b config.Keybindings) []string { return b.Diff.Expand },
			nav:  func(m *model, _ string) tea.Cmd { return k.toggleExpanded(m) },
		},
		{
			id:   actionDiffWrap,
			keys: func(b config.Keybindings) []string { return b.Diff.Wrap },
			nav:  func(m *model, _ string) tea.Cmd { return k.toggleWrap(m) },
		},
	}
}

// directionOf is back for a press of the backward keys and forward for any other.
func directionOf(pressed string, backward []string) int {
	if isBound(pressed, backward) {
		return -1
	}

	return 1
}

// legend is the diff's own rows of a screen's legend, in reading order.
func (k diffKeys) legend() []screenLegendRow {
	return []screenLegendRow{
		navMoveRow("move"),
		navPageRow(),
		diffFileRow(),
		diffSearchRow(),
		diffMatchRow(),
		diffPanRow(),
		navJumpRow(),
		{
			of:   []actionID{actionDiffExpand},
			desc: func(*model) string { return k.expandDesc() },
		},
		diffWrapRow(),
	}
}

func diffFileRow() screenLegendRow {
	return screenLegendRow{
		of:   []actionID{actionDiffFile},
		desc: func(*model) string { return "next/previous file" },
		label: func(k config.Keybindings) string {
			return headLabel(k.Diff.NextFile) + "/" + headLabel(k.Diff.PrevFile)
		},
	}
}

func diffSearchRow() screenLegendRow {
	return screenLegendRow{
		of:   []actionID{actionFilterOpen},
		desc: func(*model) string { return "search" },
	}
}

func diffMatchRow() screenLegendRow {
	return screenLegendRow{
		of:   []actionID{actionDiffMatch},
		desc: func(*model) string { return "next/previous match" },
		label: func(k config.Keybindings) string {
			return headLabel(k.Diff.NextMatch) + "/" + headLabel(k.Diff.PrevMatch)
		},
	}
}

// diffPanRow is panning and its reset, which share a row.
func diffPanRow() screenLegendRow {
	return screenLegendRow{
		of:   []actionID{actionDiffPan, actionDiffPanReset},
		desc: func(*model) string { return "pan sideways" },
		label: func(k config.Keybindings) string {
			return headLabel(k.Diff.PanLeft) + "/" + headLabel(k.Diff.PanRight) + "/" + keyLabel(k.Diff.PanReset)
		},
	}
}

// diffWrapRow is the key that wraps long lines or cuts them off again.
func diffWrapRow() screenLegendRow {
	return screenLegendRow{
		of:   []actionID{actionDiffWrap},
		desc: wrapDesc,
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

	file, ok := view.GetDrawnFileAt(k.pane.Cursor())
	if ok && view.IsExpanded(file.Path()) {
		return "show the changes"
	}

	return "show the whole file"
}

// updateFilter is the search prompt taking keys.
func (k diffKeys) updateFilter(m *model, key tea.KeyPressMsg) tea.Cmd {
	if key.String() == "ctrl+c" {
		return m.hide()
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
func (k diffKeys) jumpMatch(m *model, direction int) tea.Cmd {
	moved, found := k.pane.JumpMatch(direction, k.window(m))
	if !found {
		said := "nothing to search for yet"
		if k.filter.isStanding() {
			said = "no matches"
		}
		m.note(messageWarning, said)

		return nil
	}

	if !moved {
		return nil
	}

	return k.land(m)
}

// toggleWrap wraps lines wider than the pane, or cuts them off again.
func (k diffKeys) toggleWrap(m *model) tea.Cmd {
	m.diffWrap = !m.diffWrap
	if k.pane.View() != nil {
		k.pane.ScrollToCursor(k.window(m))
	}

	return nil
}

// toggleExpanded draws the whole of the file the cursor is in, or gives back the changed lines
// and the few around them.
func (k diffKeys) toggleExpanded(m *model) tea.Cmd {
	if k.refuseExpand != nil {
		if refused := k.refuseExpand(m); refused != "" {
			m.note(messageWarning, refused)

			return nil
		}
	}

	switch k.pane.ToggleExpanded(k.window(m)) {
	case diffview.ExpandNothing:
		return nil
	case diffview.ExpandNoMore:
		m.note(messageWarning, "there is no more of that file to show")

		return nil
	case diffview.ExpandWhole:
		m.note(messageInfo, "showing the whole file")
	case diffview.ExpandChanged:
		m.note(messageInfo, "showing the changed lines")
	}

	return k.land(m)
}

func (k diffKeys) moveTo(m *model, row int) tea.Cmd {
	if !k.pane.MoveTo(row, k.window(m)) {
		return nil
	}

	return k.land(m)
}

func (k diffKeys) step(m *model, direction, rows int) tea.Cmd {
	if !k.pane.Step(direction, rows, k.window(m)) {
		return nil
	}

	return k.land(m)
}

// land tells the screen the reader is standing somewhere new.
func (k diffKeys) land(m *model) tea.Cmd {
	if k.moved == nil {
		return nil
	}

	return k.moved(m)
}

func (k diffKeys) moveBy(m *model, rows int) tea.Cmd {
	if rows < 0 {
		return k.step(m, -1, -rows)
	}

	return k.step(m, 1, rows)
}

func (k diffKeys) moveToEnd(m *model, direction int) tea.Cmd {
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

// diffMatchCount is where in the search the cursor is standing, drawn under the pattern.
func diffMatchCount(pane *diffview.Pane, filter filterState) string {
	view := pane.View()
	if view == nil || !filter.isStanding() {
		return ""
	}

	where := label.Counted(view.MatchCount(), "match")
	if at, ok := view.MatchNumber(pane.Cursor()); ok {
		where = label.OfCount(at, view.MatchCount())
	}

	// A match inside a file folded away is on no row, so stepping through the matches never
	// reaches it, and it is counted apart rather than left to look like nothing was found.
	if hidden := view.HiddenMatches(); hidden > 0 {
		where += fmt.Sprintf(separated+"%d in viewed files", hidden)
	}

	return dimStyle().Render("  "+where) + "\n\n"
}
