package review

import (
	"strings"

	"komrad/internal/ui/column"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/layout"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/reader"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/zone"

	"github.com/charmbracelet/x/ansi"
)

// The words the review shows while there is no patch to render, which the sidebar's head shows as well.
const (
	loadingWord = "loading the diff…"
	emptyWord   = "nothing changed in this worktree"
)

// noFileWord is what the tree says when its search matches no file.
const noFileWord = "no file matches"

// diffFloor is the narrowest a diff is worth rendering at: the gutter and enough of a line beside
// it to read.
const diffFloor = diffview.GutterWidth + 20

// Column is the diff rendered beside the review's own threads, whose row budget is what the
// band over it leaves.
func (r *Screen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: diffFloor,
		// The editor is another process holding the stage, so the panel owns the window only while
		// there is none.
		Holds:  func() bool { return !r.edit.atEditor },
		Beside: func() (panel.AgentRow, bool) { return r.writerPane(in) },
		Rows: func(split column.Split) int {
			// The band stands on the diff's own rows rather than the window's, so what is left is
			// what the patch is rendered in and what the cursor is scrolled against.
			return split.RowCount - strings.Count(r.bandView(in), "\n")
		},
	}
}

// bandView is what stands over the diff: the range it runs over and the pattern it is searched for.
func (r Screen) bandView(in screen.Inputs) string {
	search := r.diff.filter.View(r.deps.Glyphs)
	if in.Room.ShouldMarkZones {
		search = zone.MarkKey(zone.Search, r.measureBandWidth(in), 0, zone.SearchList) + search
	}

	return r.rangeView(in) + search + r.diff.RenderMatchCount(r.diff.filter.IsStanding())
}

// rangeView names both ends of the range on screen and, past its end, the snapshot new comments go on.
func (r Screen) rangeView(in screen.Inputs) string {
	if r.diff.View() == nil {
		return ""
	}

	said := paint.Panel.Style(scopeOnTone).Bold(true).Render(r.diff.rangeSaid())
	if newest := r.diff.newestSaid(); newest != "" {
		said += "   " + paint.Panel.Style(historyTone).Render(newest)
	}

	return ansi.Wrap(said, r.measureBandWidth(in), "") + "\n"
}

func (r Screen) measureBandWidth(in screen.Inputs) int {
	if split := in.Room.WindowSplit; split.IsDivided {
		return split.ColumnWidth
	}

	return layout.LineWidth(in.Room.Width)
}

// diffNotice is what stands in the diff's own column before a patch does.
func (r Screen) diffNotice(in screen.Inputs) string {
	if r.loading {
		return in.Room.SpinnerFrame + " _" + loadingWord + "_"
	}

	return "_" + emptyWord + "_"
}

// diffView is the screenful of the patch beside the sidebar, starting at top, with the cursor's
// own row dim while the keys are on the record rather than on the diff.
func (r Screen) diffView(in screen.Inputs, split column.Split) string {
	view := r.diff.View()
	if view == nil || view.Len() == 0 {
		// This is the larger half of the window, so it says what is being waited on rather than
		// standing empty under a band, and stands rather than collapsing so the frame does not
		// shift under the reader when the patch lands.
		return reader.Page{Rows: paint.DocStyledRows(r.diffNotice(in), split.ColumnWidth, paint.Panel.Stage())}.View(split.RowCount)
	}

	held := diffview.LiveCursor
	if !r.IsDiffFocused(in) {
		held = diffview.DimCursor
	}

	window := r.diffWindow(in)

	lines := r.diff.Render(window, held)
	if in.Room.ShouldMarkZones {
		lines = r.diff.Mark(window, lines)
	}

	return strings.Join(lines, "\n")
}
