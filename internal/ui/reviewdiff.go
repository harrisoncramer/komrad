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

// reviewDiffView is the screenful of the patch beside the sidebar, starting at top, with the
// cursor's own row dim while the keys are on the record rather than on the diff.
func (m model) reviewDiffView(split columnSplit) string {
	view := m.review.view
	if view == nil {
		// The sidebar says what is being waited on, so the column stands empty rather than saying
		// it again, and stands rather than collapsing so the frame does not shift under the reader
		// when the patch lands.
		return blankColumn(split)
	}

	start := max(min(m.review.top, view.Len()-split.rows), 0)

	held := review.LiveCursor
	if m.isDetailsFocused() {
		held = review.DimCursor
	}

	// Only the rows the pane can show are drawn, since a nine thousand row diff takes a tenth of
	// a second to draw whole and panning would pay that on every keypress.
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
