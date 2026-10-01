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
	// height is the rows the pane is drawn in.
	height func(panelShared) int
	// moved is what the screen does once the reader lands somewhere new, and is nil on a screen
	// with nothing to do.
	moved func(navHost, panelShared) tea.Cmd
	// refuseExpand says why the file under the cursor may not be drawn whole, and is nil on a
	// screen that never refuses.
	refuseExpand func(panelShared) string
}

func (k diffKeys) table() []screenAction {
	return []screenAction{
		{
			id:   actionMove,
			keys: func(b config.Keybindings) []string { return slices.Concat(b.Nav.Up, b.Nav.Down) },
			nav: func(h navHost, s panelShared, pressed string) tea.Cmd {
				return k.step(h, s, directionOf(pressed, s.keys.Nav.Up), 1)
			},
		},
		{
			id:   actionJump,
			keys: func(b config.Keybindings) []string { return slices.Concat(b.Nav.Top, b.Nav.Bottom) },
			nav: func(h navHost, s panelShared, pressed string) tea.Cmd {
				if isBound(pressed, s.keys.Nav.Bottom) {
					return k.moveTo(h, s, k.pane.View().GetLastRow())
				}

				return k.moveTo(h, s, k.pane.View().GetFirstRow())
			},
		},
		{
			id:   actionNavPage,
			keys: func(b config.Keybindings) []string { return slices.Concat(b.Nav.PageUp, b.Nav.PageDown) },
			nav: func(h navHost, s panelShared, pressed string) tea.Cmd {
				return k.step(h, s, directionOf(pressed, s.keys.Nav.PageUp), navPageStep)
			},
		},
		{
			id:   actionColumnScroll,
			keys: func(b config.Keybindings) []string { return slices.Concat(b.Column.ScrollDown, b.Column.ScrollUp) },
			nav: func(h navHost, s panelShared, pressed string) tea.Cmd {
				return k.step(h, s, directionOf(pressed, s.keys.Column.ScrollUp), diffWheelStep)
			},
		},
		{
			id:   actionDiffFile,
			keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.NextFile, b.Diff.PrevFile) },
			nav: func(h navHost, s panelShared, pressed string) tea.Cmd {
				if !k.pane.JumpFile(directionOf(pressed, s.keys.Diff.PrevFile), k.height(s)) {
					return nil
				}

				return k.land(h, s)
			},
		},
		{
			id:   actionFilterOpen,
			keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
			nav: func(_ navHost, s panelShared, _ string) tea.Cmd {
				k.filter.typing = true
				k.filter.setQuery("")
				k.search(s)

				return nil
			},
		},
		{
			id:   actionDiffMatch,
			keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.NextMatch, b.Diff.PrevMatch) },
			nav: func(h navHost, s panelShared, pressed string) tea.Cmd {
				return k.jumpMatch(h, s, directionOf(pressed, s.keys.Diff.PrevMatch))
			},
		},
		{
			id:   actionDiffPan,
			keys: func(b config.Keybindings) []string { return slices.Concat(b.Diff.PanLeft, b.Diff.PanRight) },
			nav: func(_ navHost, s panelShared, pressed string) tea.Cmd {
				k.pane.PanBy(directionOf(pressed, s.keys.Diff.PanLeft) * panStep(s))

				return nil
			},
		},
		{
			id:   actionDiffPanReset,
			keys: func(b config.Keybindings) []string { return b.Diff.PanReset },
			nav: func(navHost, panelShared, string) tea.Cmd {
				k.pane.PanBy(-k.pane.Pan())

				return nil
			},
		},
		{
			id:   actionDiffExpand,
			keys: func(b config.Keybindings) []string { return b.Diff.Expand },
			nav:  func(h navHost, s panelShared, _ string) tea.Cmd { return k.toggleExpanded(h, s) },
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
		diffMoveRow(),
		diffPageRow(),
		diffFileRow(),
		diffSearchRow(),
		diffMatchRow(),
		diffPanRow(),
		diffJumpRow(),
		{
			of:   []actionID{actionDiffExpand},
			desc: func(panelShared) string { return k.expandDesc() },
		},
	}
}

func diffMoveRow() screenLegendRow {
	return screenLegendRow{
		of:    []actionID{actionMove},
		desc:  func(panelShared) string { return "move" },
		label: moveLabel,
		strip: true,
	}
}

func diffPageRow() screenLegendRow {
	return screenLegendRow{
		of:    []actionID{actionNavPage},
		desc:  func(panelShared) string { return "page" },
		label: navPageLabel,
	}
}

func diffFileRow() screenLegendRow {
	return screenLegendRow{
		of:   []actionID{actionDiffFile},
		desc: func(panelShared) string { return "next/previous file" },
		label: func(k config.Keybindings) string {
			return headLabel(k.Diff.NextFile) + "/" + headLabel(k.Diff.PrevFile)
		},
	}
}

func diffSearchRow() screenLegendRow {
	return screenLegendRow{
		of:   []actionID{actionFilterOpen},
		desc: func(panelShared) string { return "search" },
	}
}

func diffMatchRow() screenLegendRow {
	return screenLegendRow{
		of:   []actionID{actionDiffMatch},
		desc: func(panelShared) string { return "next/previous match" },
		label: func(k config.Keybindings) string {
			return headLabel(k.Diff.NextMatch) + "/" + headLabel(k.Diff.PrevMatch)
		},
	}
}

// diffPanRow is panning and its reset, which share a row.
func diffPanRow() screenLegendRow {
	return screenLegendRow{
		of:   []actionID{actionDiffPan, actionDiffPanReset},
		desc: func(panelShared) string { return "pan sideways" },
		label: func(k config.Keybindings) string {
			return headLabel(k.Diff.PanLeft) + "/" + headLabel(k.Diff.PanRight) + "/" + keyLabel(k.Diff.PanReset)
		},
	}
}

func diffJumpRow() screenLegendRow {
	return screenLegendRow{
		of:    []actionID{actionJump},
		desc:  func(panelShared) string { return "top/bottom" },
		label: jumpLabel,
	}
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
func (k diffKeys) updateFilter(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	if key.String() == "ctrl+c" {
		return h.hide()
	}

	if k.filter.takeKey(key, s.register) {
		k.search(s)
	}

	return nil
}

// search runs the pattern over the diff and moves to the first thing it finds.
func (k diffKeys) search(s panelShared) {
	if k.pane.View() == nil {
		return
	}

	k.pane.Search(k.filter.pattern, k.height(s))
}

// jumpMatch steps to the next or previous line the search found, wrapping at the ends.
func (k diffKeys) jumpMatch(h navHost, s panelShared, direction int) tea.Cmd {
	moved, found := k.pane.JumpMatch(direction, k.height(s))
	if !found {
		said := "nothing to search for yet"
		if k.filter.isStanding() {
			said = "no matches"
		}
		h.note(messageWarning, said)

		return nil
	}

	if !moved {
		return nil
	}

	return k.land(h, s)
}

// toggleExpanded draws the whole of the file the cursor is in, or gives back the changed lines
// and the few around them.
func (k diffKeys) toggleExpanded(h navHost, s panelShared) tea.Cmd {
	if k.refuseExpand != nil {
		if refused := k.refuseExpand(s); refused != "" {
			h.note(messageWarning, refused)

			return nil
		}
	}

	switch k.pane.ToggleExpanded(k.height(s)) {
	case diffview.ExpandNothing:
		return nil
	case diffview.ExpandNoMore:
		h.note(messageWarning, "there is no more of that file to show")

		return nil
	case diffview.ExpandWhole:
		h.note(messageInfo, "showing the whole file")
	case diffview.ExpandChanged:
		h.note(messageInfo, "showing the changed lines")
	}

	return k.land(h, s)
}

func (k diffKeys) moveTo(h navHost, s panelShared, row int) tea.Cmd {
	if !k.pane.MoveTo(row, k.height(s)) {
		return nil
	}

	return k.land(h, s)
}

func (k diffKeys) step(h navHost, s panelShared, direction, rows int) tea.Cmd {
	if !k.pane.Step(direction, rows, k.height(s)) {
		return nil
	}

	return k.land(h, s)
}

// land tells the screen the reader is standing somewhere new.
func (k diffKeys) land(h navHost, s panelShared) tea.Cmd {
	if k.moved == nil {
		return nil
	}

	return k.moved(h, s)
}

// scrollColumn moves the cursor a number of rows up the diff for a negative count and down it
// for a positive one.
func (k diffKeys) scrollColumn(h navHost, s panelShared, rows int) tea.Cmd {
	if rows < 0 {
		return k.step(h, s, -1, -rows)
	}

	return k.step(h, s, 1, rows)
}

// panStep is a third of the pane, or the smallest jump until the pane is measured.
func panStep(s panelShared) int {
	split, ok := s.draw.screenColumnSplit()
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
