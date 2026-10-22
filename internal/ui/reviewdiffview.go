package ui

import (
	"strings"

	"komrad/internal/ui/column"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

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
func reviewColumn(m *model, in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: diffFloor,
		// The editor is another process holding the stage, so the panel owns the window only while
		// there is none.
		Holds:  func() bool { return !m.review.hold.atEditor },
		Beside: func() (panel.AgentRow, bool) { return m.review.writerPane(m) },
		Rows: func(split column.Split) int {
			// The band stands on the diff's own rows rather than the window's, so what is left is
			// what the patch is rendered in and what the cursor is scrolled against.
			return split.Rows - strings.Count(m.review.bandView(in), "\n")
		},
	}
}

// bandView is what stands over the diff: the range it runs over and the pattern it is searched for.
func (r reviewScreen) bandView(in screen.Inputs) string {
	search := r.diff.filter.view(r.deps.glyphs)
	if in.Room.Marks {
		search = searchMark(stopList, r.measureBandWidth(in), search)
	}

	return r.rangeView(in) + search + diffMatchCount(&r.diff.Pane, r.diff.filter)
}

// rangeView names both ends of the range on screen and, past its end, the snapshot new comments go on.
func (r reviewScreen) rangeView(in screen.Inputs) string {
	if r.diff.View() == nil {
		return ""
	}

	said := paint.Panel.Style(reviewScopeOnTone).Bold(true).Render(r.diff.rangeSaid())
	if newest := r.diff.newestSaid(); newest != "" {
		said += "   " + paint.Panel.Style(reviewHistoryTone).Render(newest)
	}

	return ansi.Wrap(said, r.measureBandWidth(in), "") + "\n"
}

func (r reviewScreen) measureBandWidth(in screen.Inputs) int {
	if split := in.Room.Geometry; split.On {
		return split.Column
	}

	return lineWidthOf(in.Room.Width)
}

// diffNotice is what stands in the diff's own column before a patch does.
func (r reviewScreen) diffNotice(in screen.Inputs) string {
	if r.loading {
		return in.Room.Spinner + " _" + reviewLoadingWord + "_"
	}

	return "_" + reviewEmptyWord + "_"
}

// diffView is the screenful of the patch beside the sidebar, starting at top, with the cursor's
// own row dim while the keys are on the record rather than on the diff.
func (r reviewScreen) diffView(m *model, in screen.Inputs, split column.Split) string {
	view := r.diff.View()
	if view == nil || view.Len() == 0 {
		// This is the larger half of the window, so it says what is being waited on rather than
		// standing empty under a band, and stands rather than collapsing so the frame does not
		// shift under the reader when the patch lands.
		return docWindow{rows: paint.DocStyledRows(r.diffNotice(in), split.Column, reviewColumnStyle())}.view(split.Rows)
	}

	held := diffview.LiveCursor
	if !r.isDiffFocused(m, in) {
		held = diffview.DimCursor
	}

	window := r.diffWindow(in)

	lines := r.diff.Render(window, held)
	if in.Room.Marks {
		lines = markDiff(&r.diff.Pane, window, lines)
	}

	return strings.Join(lines, "\n")
}

// reviewColumnStyle is the markdown a notice standing in for the patch is rendered in.
func reviewColumnStyle() glamouransi.StyleConfig {
	return paint.Panel.Stage()
}
