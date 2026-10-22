package ui

import (
	"slices"
	"strings"

	"komrad/internal/panes"
	"komrad/internal/ui/zone"
	"komrad/internal/view"

	"charm.land/lipgloss/v2"
)

const (
	zoneList       zone.Name = "list"
	zoneColumn     zone.Name = "column"
	zoneDetails    zone.Name = "details"
	zoneBlock      zone.Name = "block"
	zoneSide       zone.Name = "side"
	zoneViews      zone.Name = "views"
	zoneKinds      zone.Name = "kinds"
	zoneTabs       zone.Name = "tabs"
	zoneFiles      zone.Name = "files"
	zoneMessages   zone.Name = "messages"
	zoneDoc        zone.Name = "doc"
	zoneMessage    zone.Name = "message"
	zoneLog        zone.Name = "log"
	zoneDiff       zone.Name = "diff"
	zoneMenu       zone.Name = "menu"
	zoneParentMenu zone.Name = "parentmenu"
)

// markZone wraps a block in the marks that say where it was rendered, while the panel is rendering a
// marked frame.
func (m *model) markZone(name zone.Name, block string, width int) string {
	if !m.term.markingZones {
		return block
	}

	return zone.MarkBlock(name, block, width)
}

// sidebarBlock is a block the panel rendered inside its own pane that the picker can land on.
type sidebarBlock struct {
	stop stop
	rect panes.Rect
}

// findSidebarBlocks renders the panel once with its blocks marked and reads where each one landed.
func (m *model) findSidebarBlocks() []sidebarBlock {
	marked := *m
	marked.term.markingZones = true

	zones := zone.Scan(marked.renderInset(marked.insideView()))
	stops := m.listStops()
	height := m.term.height

	var blocks []sidebarBlock

	details, hasDetails := zones[zoneDetails]
	if hasDetails && slices.Contains(stops, stopRecord) {
		blocks = append(blocks, sidebarBlock{stop: stopRecord, rect: details})
		height = details.Top
	}

	list := panes.Rect{
		Left:   0,
		Top:    0,
		Width:  m.term.width,
		Height: height,
	}
	column, hasColumn := zones[zoneColumn]
	if hasColumn {
		list.Width = column.Left - dividerCols
	}

	if marked, ok := zones[zoneList]; ok {
		list = marked
	}

	block, hasBlock := zones[zoneBlock]
	if !hasBlock {
		block, hasBlock = column, hasColumn
	}

	if hasBlock && slices.Contains(stops, stopBlock) {
		blocks = append(blocks, sidebarBlock{stop: stopBlock, rect: block})
	}

	return append([]sidebarBlock{{stop: stopList, rect: list}}, blocks...)
}

// clipRect is the part of rect inside bounds, and whether any of it is.
func clipRect(rect, bounds panes.Rect) (panes.Rect, bool) {
	left, top := max(rect.Left, bounds.Left), max(rect.Top, bounds.Top)
	right := min(rect.Left+rect.Width, bounds.Left+bounds.Width)
	bottom := min(rect.Top+rect.Height, bounds.Top+bounds.Height)

	if right <= left || bottom <= top {
		return panes.Rect{}, false
	}

	return panes.Rect{
		Pane:   rect.Pane,
		Left:   left,
		Top:    top,
		Width:  right - left,
		Height: bottom - top,
	}, true
}

// isInRect is whether the cell at x, y falls inside rect.
func isInRect(rect panes.Rect, x, y int) bool {
	return x >= rect.Left && x < rect.Left+rect.Width && y >= rect.Top && y < rect.Top+rect.Height
}

func (m *model) rowMarker(name zone.Name, rows []view.Row, width int) func(int, string) string {
	if !m.term.markingZones {
		return nil
	}

	return func(index int, row string) string {
		key := rows[index].Key
		if key == "" {
			return row
		}

		nth := 0
		for _, earlier := range rows[:index] {
			if earlier.Key == key {
				nth++
			}
		}

		return zone.MarkKey(name, width, nth, key) + row
	}
}

func (m *model) spanMarker(name zone.Name, keys []string) func(int, string) string {
	if !m.term.markingZones {
		return nil
	}

	return func(index int, span string) string {
		return markKeyedSpan(name, keys[index], span)
	}
}

func markKeyedSpan(name zone.Name, key, span string) string {
	if key == "" {
		return span
	}

	return zone.MarkKey(name, lipgloss.Width(span), 0, key) + span
}

func (m *model) markSpan(name zone.Name, key, span string) string {
	if !m.term.markingZones {
		return span
	}

	return zone.MarkKey(name, lipgloss.Width(span), 0, key) + span
}

func (m *model) markSearch(key string, width int, line string) string {
	if !m.term.markingZones {
		return line
	}

	return zone.MarkKey(zone.Search, width, 0, key) + line
}

func (m *model) markRow(name zone.Name, width int, key, row string) string {
	if !m.term.markingZones {
		return row
	}

	return zone.MarkKey(name, width, 0, key) + row
}

func (m *model) findKeyAt(x, y int) (zone.Hit, bool) {
	marked := *m
	marked.term.markingZones = true

	lines := strings.Split(marked.renderInset(marked.insideView()), "\n")
	if y < 0 || y >= len(lines) {
		return zone.Hit{}, false
	}

	return zone.HitAt(lines[y], x)
}
