package ui

import (
	"slices"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	"github.com/charmbracelet/x/ansi"
)

const komradName = "komrad"

// viewsBarFloor is the shortest pane the switcher is rendered in.
const viewsBarFloor = 20

// viewsBar is the logo and the switcher rendered over a list, and under them the trail down to the
// row or page the cursor is on.
func (m *model) viewsBar() string {
	return m.Bars()[0]
}

// Bars is every form the bar can take, tallest first.
func (m *model) Bars() []string {
	trail, _, opened := m.activeCrumbs()
	if m.term.height > 0 && m.term.height < viewsBarFloor {
		if m.isMessagesFlashing() && m.paneWidth() >= 3 {
			return []string{m.engineLine(m.paneWidth()) + "\n"}
		}

		if !opened {
			return []string{""}
		}

		return []string{trail.view(paint.Panel, m.paneWidth()) + "\n"}
	}

	lines := m.switcherLines(trail)
	logo, engine := lines[0], lines[1]

	return slices.Compact([]string{
		strings.Join(lines, "\n") + "\n",
		logo + "\n" + engine + "\n",
		engine + "\n",
	})
}

// switcherLines is the logo and, on a screen that renders the switcher, the row of views.
func (m *model) switcherLines(trail crumbs) []string {
	logo := paint.Panel.Title(glyph.Logo + " " + komradName)
	if path := trail.view(paint.Panel, 0); path != "" {
		logo += paint.Panel.Style(paint.ToneMuted).Render(" "+glyph.Crumb+" ") + path
	}

	lines := []string{logo, m.engineLine(m.paneWidth()), ""}
	if rendersSwitcher(m.screen) {
		lines = append(lines, m.viewsRow())
	}
	if width := m.paneWidth(); width > 0 {
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, width, "…")
		}
	}

	return lines
}

func (m *model) viewsRow() string {
	labels := xslice.Map(viewEntries(), func(entry viewEntry) string { return entry.label })
	row := stops{
		labels: labels,
		focus:  noStop,
		lit:    noStop,
		mark:   m.spanMarker(zoneViews, labels),
	}
	if m.isViewsOpen() {
		row.focus = m.viewIndex()
	} else {
		row.lit = m.viewIndex()
	}

	return row.view()
}
