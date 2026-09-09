package ui

import (
	"strings"

	"weave/internal/review"
)

// reviewDiffFloor is the narrowest the diff is worth drawing at, which is the gutter and enough
// of a line beside it to read.
const reviewDiffFloor = review.GutterWidth + 20

// reviewColumn is the diff drawn beside the review's own threads, whose row budget is what the
// band over it leaves.
func reviewColumn() columnSpec {
	return columnSpec{
		floor: reviewDiffFloor,
		rows: func(m model, split columnSplit) int {
			// The band stands on the diff's own rows rather than the window's, so what is left is
			// what the patch is drawn in and what the cursor is scrolled against.
			return split.rows - strings.Count(m.reviewBandView(split.column), "\n")
		},
	}
}

// What the review says while there is no patch to draw, of which Loading and Empty are said by
// the sidebar's head as well, while Broken is the column's own short form.
const (
	reviewLoadingWord = "loading the diff…"
	reviewEmptyWord   = "nothing changed in this worktree"
	reviewBrokenWord  = "the diff could not be read"
)

// reviewDiffNotice is what stands in the diff's own column before a patch does, which the sidebar
// says as well but in a fraction of the room and away from where the reader is looking.
func (m model) reviewDiffNotice() string {
	switch {
	case m.review.err != nil:
		return "_" + reviewBrokenWord + "_"
	case m.review.loading:
		return "_" + reviewLoadingWord + "_"
	default:
		return "_" + reviewEmptyWord + "_"
	}
}

// reviewDiffView is the screenful of the patch beside the sidebar, starting at top, with the
// cursor's own row dim while the keys are on the record rather than on the diff.
func (m model) reviewDiffView(split columnSplit) string {
	view := m.review.view
	if view == nil || view.Len() == 0 {
		// This is the larger half of the window, so it says what is being waited on rather than
		// standing empty under a band, and stands rather than collapsing so the frame does not
		// shift under the reader when the patch lands.
		return docWindow{rows: docStyledRows(m.reviewDiffNotice(), split.column, m.reviewColumnStyle())}.view(split.rows)
	}

	start := max(min(m.review.top, view.Len()-split.rows), 0)

	held := review.LiveCursor
	if !m.isReviewDiffFocused() {
		held = review.DimCursor
	}

	// Only the rows the pane can show are drawn, a nine thousand row diff taking a tenth of a
	// second to draw whole.
	rows := make([]string, 0, split.rows)
	for y := start; y < min(start+split.rows, view.Len()); y++ {
		mark := review.NoCursor
		if y == m.review.cursor {
			mark = held
		}

		rows = append(rows, view.RenderRow(y, mark, split.column, m.review.pan))
	}

	// Padded out rather than left short, so a diff smaller than the pane keeps its half of the
	// window instead of the sidebar sliding under it.
	for len(rows) < split.rows {
		rows = append(rows, strings.Repeat(" ", split.column))
	}

	return strings.Join(rows, "\n")
}
