package ui

import (
	"fmt"
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/lib/dates"

	"charm.land/glamour/v2/ansi"
)

// What the review says while there is no patch to render, which the sidebar's head says as well.
const (
	reviewLoadingWord = "loading the diff…"
	reviewEmptyWord   = "nothing changed in this worktree"
)

// reviewNoFileWord is what the tree says when its search matches no file.
const reviewNoFileWord = "no file matches"

// reviewColumn is the diff rendered beside the review's own threads, whose row budget is what the
// band over it leaves.
func reviewColumn() columnSpec {
	return columnSpec{
		floor: diffFloor,
		// The editor is another process holding the stage, so the panel owns the window only while
		// there is none.
		holds: func(m *model) bool { return !m.review.hold.atEditor },
		rows: func(m *model, split columnSplit) int {
			// The band stands on the diff's own rows rather than the window's, so what is left is
			// what the patch is rendered in and what the cursor is scrolled against.
			return split.rows - strings.Count(m.review.bandView(m), "\n")
		},
	}
}

// bandView is what stands over the diff: whose work is being read, the round it is frozen on, and
// the pattern it is searched for.
func (r reviewScreen) bandView(m *model) string {
	return paint.Panel.Title("Review"+separated+r.agentName) + "\n" +
		r.frozenView(m) +
		r.diff.filter.view(m.glyphs) + diffMatchCount(&r.diff.Pane, r.diff.filter)
}

func (r reviewScreen) frozenView(m *model) string {
	round, ok := r.isFrozen()
	if !ok {
		return ""
	}

	said := fmt.Sprintf("Frozen on round %d from %s until you send your review%s%s shows the latest",
		round.GetOrdinal(), label.Friendly(dates.Parse(round.GetCreatedAt())),
		separated, m.menuChordLabel(m.keys.Review.Reload))

	var out strings.Builder
	for _, row := range paint.WrapRows(said, r.measureBandWidth(m)) {
		out.WriteString(paint.Panel.Style(reviewFrozenTone).Render(row) + "\n")
	}

	return out.String()
}

func (r reviewScreen) measureBandWidth(m *model) int {
	if split, ok := m.columnGeometry(); ok {
		return split.column
	}

	return lineWidthOf(m.paneWidth())
}

// diffNotice is what stands in the diff's own column before a patch does.
func (r reviewScreen) diffNotice(m *model) string {
	if r.loading {
		return m.loading.frame() + " _" + reviewLoadingWord + "_"
	}

	return "_" + reviewEmptyWord + "_"
}

// diffView is the screenful of the patch beside the sidebar, starting at top, with the cursor's
// own row dim while the keys are on the record rather than on the diff.
func (r reviewScreen) diffView(m *model, split columnSplit) string {
	view := r.diff.View()
	if view == nil || view.Len() == 0 {
		// This is the larger half of the window, so it says what is being waited on rather than
		// standing empty under a band, and stands rather than collapsing so the frame does not
		// shift under the reader when the patch lands.
		return docWindow{rows: paint.DocStyledRows(r.diffNotice(m), split.column, reviewColumnStyle())}.view(split.rows)
	}

	held := diffview.LiveCursor
	if !r.isDiffFocused(m) {
		held = diffview.DimCursor
	}

	return strings.Join(r.diff.Render(r.diffWindow(m), held), "\n")
}

// reviewColumnStyle is the markdown a notice standing in for the patch is rendered in.
func reviewColumnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}
