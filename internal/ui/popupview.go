package ui

import (
	"strings"

	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// overlayPopup lays a popup rendered by renderPopup over an area at its slot.
func overlayPopup(area, box string, slot popupSlot) string {
	return compose(lipgloss.Width(area), strings.Count(area, "\n")+1, []*lipgloss.Layer{
		lipgloss.NewLayer(area),
		lipgloss.NewLayer(box).X(slot.x).Y(slot.y).Z(1),
	})
}

// renderPopup boxes a body at a slot's width and height, showing the rows back rows from its end
// and marking what runs past the box above and below.
func renderPopup(body string, back int, slot popupSlot) string {
	room := slot.room()
	lines := splitPopupRows(body)
	window := paint.WindowOf(len(lines), room, func(visible int) int {
		if back >= len(lines)-room {
			return 0
		}

		return max(len(lines)-visible-back, 0)
	})

	shown := paint.RenderWindow(lines[window.Start:window.End], window, len(lines), paint.Panel.Style(paint.ToneMuted))
	rows := strings.Split(strings.TrimSuffix(shown, "\n"), "\n")
	for len(rows) < room {
		rows = append(rows, "")
	}

	padded := xslice.Map(rows, func(row string) string {
		return fitPopupRow(row, slot.width-popupChrome)
	})

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paint.Panel.Color(paint.ToneMuted)).
		Padding(0, 1).
		Render(strings.Join(padded, "\n"))
}

// fitPopupRow cuts or pads a row to the popup's inner width, so the area under it never shows
// through.
func fitPopupRow(row string, width int) string {
	row = ansi.Truncate(row, width, "…")

	return row + strings.Repeat(" ", max(width-lipgloss.Width(row), 0))
}
