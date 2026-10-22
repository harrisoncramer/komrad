package ui

import (
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"

	glamouransi "charm.land/glamour/v2/ansi"
	"github.com/charmbracelet/x/ansi"
)

// The words the review shows while there is no patch to render, which the sidebar's head shows as well.
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
		holds:  func(m *model) bool { return !m.review.hold.atEditor },
		beside: func(m *model) (panel.AgentRow, bool) { return m.review.writerPane(m) },
		rows: func(m *model, split columnSplit) int {
			// The band stands on the diff's own rows rather than the window's, so what is left is
			// what the patch is rendered in and what the cursor is scrolled against.
			return split.rows - strings.Count(m.review.bandView(m), "\n")
		},
	}
}

// bandView is what stands over the diff: the range it runs over and the pattern it is searched for.
func (r reviewScreen) bandView(m *model) string {
	return r.rangeView(m) +
		m.markSearch(stopList, r.measureBandWidth(m), r.diff.filter.view(r.deps.glyphs)) + diffMatchCount(&r.diff.Pane, r.diff.filter)
}

// rangeView names both ends of the range on screen and, past its end, the snapshot new comments go on.
func (r reviewScreen) rangeView(m *model) string {
	if r.diff.View() == nil {
		return ""
	}

	said := paint.Panel.Style(reviewScopeOnTone).Bold(true).Render(r.diff.rangeSaid())
	if newest := r.diff.newestSaid(); newest != "" {
		said += "   " + paint.Panel.Style(reviewHistoryTone).Render(newest)
	}

	return ansi.Wrap(said, r.measureBandWidth(m), "") + "\n"
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

	window := r.diffWindow(m)

	return strings.Join(m.markDiff(&r.diff.Pane, window, r.diff.Render(window, held)), "\n")
}

// reviewColumnStyle is the markdown a notice standing in for the patch is rendered in.
func reviewColumnStyle() glamouransi.StyleConfig {
	return paint.Panel.Stage()
}
