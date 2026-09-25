package ui

import (
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
)

// reviewDiff is the patch on screen and where the reader is standing in it.
type reviewDiff struct {
	view   *diffview.View
	cursor int
	pan    int
	// top is the first row the pane is showing, held rather than derived from the cursor.
	top int

	// pinTop holds the window where a file jump put it until the cursor moves.
	pinTop bool
	// pinFloor is the row a file jump put at the top of the pane, which the window stays at or
	// below while the cursor is inside that file.
	pinFloor int

	// filter is the pattern being searched for in the diff.
	filter filterState

	// restore is where the cursor was standing when the patch was read again.
	restore *diffview.Place
	// expanded is the files drawn whole when the patch was put down to be read again.
	expanded map[string]bool

	// since says how much of the diff is new to the reader.
	since string

	// base is the commit the diff on screen is measured from, empty for the point the branch
	// left main.
	base string
	// baseLabel names that commit for the band, empty while the diff is measured from the fork
	// point.
	baseLabel string
	// head is the commit the diff on screen is measured to, empty for the newest snapshot.
	head string
	// baseRound is the round the narrowed base came from, nil while there is none to offer.
	baseRound *agentv1.ReviewRound
	// baseAt is when the person released the review that round was given against, in clock terms.
	baseAt string
	// pinned says the diff is the snapshot the reader's unsent comments were written against
	// rather than the work as it now stands.
	pinned bool

	// chapters are the rounds of the review the two step keys walk, oldest first.
	chapters []reviewChapter
	// chapter is which of those is on screen, negative for the whole change.
	chapter int

	// followedMove is the move the diff last caught up on.
	followedMove string
}

// reviewDiffFloor is the narrowest the diff is worth drawing at: the gutter and enough of a line
// beside it to read.
const reviewDiffFloor = diffview.GutterWidth + 20

// reviewColumn is the diff drawn beside the review's own threads, whose row budget is what the
// band over it leaves.
func reviewColumn() columnSpec {
	return columnSpec{
		floor: reviewDiffFloor,
		// The editor is another process holding the stage, so the panel owns the window only while
		// there is none.
		holds: func(m model) bool { return !m.review.pass.atEditor },
		rows: func(m model, split columnSplit) int {
			// The band stands on the diff's own rows rather than the window's, so what is left is
			// what the patch is drawn in and what the cursor is scrolled against.
			return split.rows - strings.Count(m.review.bandView(m.shared(), split.column), "\n")
		},
	}
}

// What the review says while there is no patch to draw, which the sidebar's head says as well.
const (
	reviewLoadingWord = "loading the diff…"
	reviewEmptyWord   = "nothing changed in this worktree"
)

// diffNotice is what stands in the diff's own column before a patch does.
func (r reviewScreen) diffNotice() string {
	if r.loading {
		return "_" + reviewLoadingWord + "_"
	}

	return "_" + reviewEmptyWord + "_"
}

// diffView is the screenful of the patch beside the sidebar, starting at top, with the cursor's
// own row dim while the keys are on the record rather than on the diff.
func (r reviewScreen) diffView(s panelShared, split columnSplit) string {
	view := r.diff.view
	if view == nil || view.Len() == 0 {
		// This is the larger half of the window, so it says what is being waited on rather than
		// standing empty under a band, and stands rather than collapsing so the frame does not
		// shift under the reader when the patch lands.
		return docWindow{rows: paint.DocStyledRows(r.diffNotice(), split.column, reviewColumnStyle())}.view(split.rows)
	}

	start := max(min(r.diff.top, view.Len()-split.rows), 0)

	held := diffview.LiveCursor
	if !r.isDiffFocused(s) {
		held = diffview.DimCursor
	}

	// Only the rows the pane can show are drawn, a nine thousand row diff taking a tenth of a
	// second to draw whole.
	rows := make([]string, 0, split.rows)
	for y := start; y < min(start+split.rows, view.Len()); y++ {
		mark := diffview.NoCursor
		if y == r.diff.cursor {
			mark = held
		}

		rows = append(rows, view.RenderRow(y, mark, split.column, r.diff.pan))
	}

	// Padded out rather than left short, so a diff smaller than the pane keeps its half of the
	// window instead of the sidebar sliding under it.
	for len(rows) < split.rows {
		rows = append(rows, strings.Repeat(" ", split.column))
	}

	return strings.Join(rows, "\n")
}
