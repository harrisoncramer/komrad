package diffview

import (
	"fmt"
	"slices"
	"strconv"

	"komrad/internal/glyph"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/zone"
)

const separated = " " + glyph.Separator + " "

// MatchCount is where in the search the cursor is standing, rendered under the pattern.
func (p *Pane) MatchCount(search filter.State) string {
	view := p.View()
	if view == nil || !search.IsStanding() {
		return ""
	}

	where := label.Counted(view.MatchCount(), "match")
	if at, ok := view.MatchNumber(p.Cursor()); ok {
		where = label.OfCount(at, view.MatchCount())
	}

	// A match inside a file folded away is on no row, so stepping through the matches never
	// reaches it, and it is counted apart rather than left to look like nothing was found.
	if hidden := view.HiddenMatches(); hidden > 0 {
		where += fmt.Sprintf(separated+"%d in viewed files", hidden)
	}

	return paint.Panel.Style(paint.ToneMuted).Render("  "+where) + "\n\n"
}

// Mark keys each rendered line by the diff row it shows.
func (p *Pane) Mark(window Window, lines []string) []string {
	marked := slices.Clone(lines)
	for i, row := range p.Rows(window) {
		marked[i] = zone.MarkKey(zone.Diff, window.Width, 0, strconv.Itoa(row)) + marked[i]
	}

	return marked
}
