package ui

import (
	"strings"

	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	reviewPopupWidth  = 92
	reviewPopupHeight = 14
	reviewPopupChrome = 4
	reviewPopupLeft   = 13
)

// reviewPopupFrame is the box a popup is drawn in over the diff, and where it stands.
type reviewPopupFrame struct {
	box string
	x   int
	y   int
}

// measurePopupWidth is the width a popup takes over a column of the width given.
func measurePopupWidth(column int) int {
	return max(min(reviewPopupWidth, column), reviewPopupChrome+1)
}

// measurePopupInner is the room inside a popup over the review's diff.
func measurePopupInner(s panelShared) int {
	column, left := s.pane, 0
	if split, ok := s.draw.screenColumnSplit(); ok {
		column, left = split.column, reviewPopupLeft
	}

	return measurePopupWidth(column-left) - reviewPopupChrome
}

// popupBody is what the popup holds where the cursor stands: the editor while the reader writes,
// the conversation on the line otherwise, and nothing on a line with no thread.
func (r reviewScreen) popupBody(s panelShared, inner int) string {
	switch {
	case r.loading || r.diff.View() == nil || r.diff.View().Len() == 0:
		return ""
	case r.threads.composing:
		return r.threads.editor.view()
	case r.threads.releasing:
		return r.releaseView(inner)
	}

	id, body, ok := r.conversationAtCursor(s, inner)
	if !ok {
		return ""
	}

	if r.threads.scrolled != id {
		return body
	}

	return cutPopupEnd(body, r.threads.scroll)
}

// conversationAtCursor is the conversation on the cursor's row, a thread of the reader's ahead of
// the forge's, wrapped to inner.
func (r reviewScreen) conversationAtCursor(s panelShared, inner int) (string, string, bool) {
	if thread, ok := r.diff.threadAtCursor(); ok {
		return thread.GetId(), r.threadView(s, thread, inner), true
	}

	if offer, ok := r.offerAtCursor(s); ok {
		return offer.GetId(), r.offerView(offer, inner), true
	}

	return "", "", false
}

// cutPopupEnd drops rows off the end of a body, never leaving fewer than the popup shows.
func cutPopupEnd(body string, rows int) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	keep := max(len(lines)-rows, min(reviewPopupHeight-2, len(lines)))

	return strings.Join(lines[:keep], "\n")
}

// drawPopup boxes a body at the popup's width and height, keeping the end of it when it runs
// longer than the box.
func drawPopup(body string, width, height int) string {
	inner := width - reviewPopupChrome
	rows := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if room := max(height-2, 1); len(rows) > room {
		rows = rows[len(rows)-room:]
	}

	for len(rows) < height-2 {
		rows = append(rows, "")
	}

	padded := xslice.Map(rows, func(row string) string {
		return fitPopupRow(row, inner)
	})

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paint.Panel.Color(paint.ToneMuted)).
		Padding(0, 1).
		Render(strings.Join(padded, "\n"))
}

// placePopup stands a popup under the cursor's row, or over it when there is no room beneath,
// inside an area of the rows given.
func placePopup(box string, cursor, rows, left int) reviewPopupFrame {
	height := paint.CountLines(box)
	y := cursor + 1
	if y+height > rows {
		y = max(cursor-height, 0)
	}

	return reviewPopupFrame{
		box: box,
		x:   left,
		y:   y,
	}
}

// overlayPopup draws a popup over an area at the width given.
func overlayPopup(area string, width int, frame reviewPopupFrame) string {
	height := strings.Count(area, "\n") + 1

	return compose(width, height, []*lipgloss.Layer{
		lipgloss.NewLayer(area),
		lipgloss.NewLayer(frame.box).X(frame.x).Y(frame.y).Z(1),
	})
}

// fitPopupRow cuts or pads a row to the popup's inner width, so the diff under it never shows
// through.
func fitPopupRow(row string, width int) string {
	row = ansi.Truncate(row, width, "…")

	return row + strings.Repeat(" ", max(width-lipgloss.Width(row), 0))
}
