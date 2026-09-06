package ui

import (
	"strings"

	"weave/internal/review"
)

// reviewDiffFloor is the narrowest the diff is worth drawing at, which is the gutter and enough
// of a line beside it to read.
const reviewDiffFloor = review.GutterWidth + 20

// reviewSplit is how the window is divided while the review holds it: the diff on one side and
// the panel's own column on the other.
type reviewSplit struct {
	diff    int
	sidebar int
	rows    int
}

// reviewSplitOf divides a window between the diff and the sidebar, widening the configured
// share to what a conversation can be read in and refusing a window that cannot hold both, where
// the sidebar takes the whole of it and the diff waits for the room.
func reviewSplitOf(width, rows, sidebar int) (reviewSplit, bool) {
	if rows <= 0 || width < nameWidth+reviewDiffFloor {
		return reviewSplit{}, false
	}

	sidebar = min(max(sidebar, nameWidth), width-reviewDiffFloor)

	return reviewSplit{diff: width - sidebar, sidebar: sidebar, rows: rows}, true
}

// reviewSplit is the window the review is drawn across, false while the screen is not up, the
// patch has not arrived, or the pane is too narrow to carry both halves.
func (m model) reviewSplit() (reviewSplit, bool) {
	if m.screen != screenReview || m.review.view == nil {
		return reviewSplit{}, false
	}

	return reviewSplitOf(m.width, m.height-m.bannerLines(), m.frame.SidebarWidth(m.width))
}

// reviewDiffView is the screenful of the patch beside the sidebar, starting at top, with the
// cursor's own row dim while the keys are on the record rather than on the diff.
func (m model) reviewDiffView(split reviewSplit) string {
	view := m.review.view
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

		rows = append(rows, view.RenderRow(y, mark, split.diff, m.review.pan))
	}

	// Padded out rather than left short, so a diff smaller than the pane keeps its half of the
	// window instead of the sidebar sliding under it.
	for len(rows) < split.rows {
		rows = append(rows, strings.Repeat(" ", split.diff))
	}

	return strings.Join(rows, "\n")
}

// paneWidth is the column the panel draws its own list, record and help in, which is the whole
// pane except while the review is holding the window and drawing the diff beside itself.
func (m model) paneWidth() int {
	if split, ok := m.reviewSplit(); ok {
		return split.sidebar
	}

	return m.width
}
