package ui

import (
	"fmt"
	"slices"
	"strconv"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/zone"
)

// diffMatchCount is where in the search the cursor is standing, rendered under the pattern.
func diffMatchCount(pane *diffview.Pane, search filter.State) string {
	view := pane.View()
	if view == nil || !search.IsStanding() {
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

	return paint.Panel.Style(paint.ToneMuted).Render("  "+where) + "\n\n"
}

func markDiff(pane *diffview.Pane, window diffview.Window, lines []string) []string {
	marked := slices.Clone(lines)
	for i, row := range pane.Rows(window) {
		marked[i] = zone.MarkKey(zoneDiff, window.Width, 0, strconv.Itoa(row)) + marked[i]
	}

	return marked
}
