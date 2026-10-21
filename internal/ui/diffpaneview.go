package ui

import (
	"fmt"
	"slices"
	"strconv"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint/label"
)

// diffMatchCount is where in the search the cursor is standing, rendered under the pattern.
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

func (m *model) markDiff(pane *diffview.Pane, window diffview.Window, lines []string) []string {
	if !m.term.markingZones {
		return lines
	}

	marked := slices.Clone(lines)
	for i, row := range pane.Rows(window) {
		marked[i] = m.markRow(zoneDiff, window.Width, strconv.Itoa(row), marked[i])
	}

	return marked
}
