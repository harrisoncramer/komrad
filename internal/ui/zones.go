package ui

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"komrad/internal/panes"

	"charm.land/lipgloss/v2"
)

// zone names a block the panel draws inside its own pane.
type zone string

const (
	zoneList    zone = "list"
	zoneColumn  zone = "column"
	zoneDetails zone = "details"
	zoneBlock   zone = "block"
)

// zoneMark is the invisible sequence a marked frame carries at each end of a zone.
var zoneMark = regexp.MustCompile("\x1b_kz([se]):([a-z]+):([0-9]*)\x1b\\\\")

// markZone wraps a block in the marks that say where it was drawn, while the panel is drawing a
// marked frame.
func (m model) markZone(name zone, block string, width int) string {
	if !m.markingZones || block == "" {
		return block
	}

	return "\x1b_kzs:" + string(name) + ":" + strconv.Itoa(width) + "\x1b\\" + block + "\x1b_kze:" + string(name) + ":\x1b\\"
}

// isMarkingZones reports whether this frame is drawn to find where its blocks landed.
func (m model) isMarkingZones() bool {
	return m.markingZones
}

// scanZones finds where each marked zone sits in a frame, in cells from its top left.
func scanZones(frame string) map[zone]panes.Rect {
	type opened struct {
		left, top, width int
	}

	starts := map[zone]opened{}
	found := map[zone]panes.Rect{}

	for row, line := range strings.Split(frame, "\n") {
		for _, at := range zoneMark.FindAllStringSubmatchIndex(line, -1) {
			name := zone(line[at[4]:at[5]])
			col := lipgloss.Width(line[:at[0]])

			if line[at[2]:at[3]] == "s" {
				width, err := strconv.Atoi(line[at[6]:at[7]])
				if err != nil {
					continue
				}

				starts[name] = opened{
					left:  col,
					top:   row,
					width: width,
				}

				continue
			}

			start, ok := starts[name]
			if !ok {
				continue
			}

			bottom := row
			if col == 0 && row > start.top {
				bottom--
			}

			found[name] = panes.Rect{
				Pane:   string(name),
				Left:   start.left,
				Top:    start.top,
				Width:  start.width,
				Height: bottom - start.top + 1,
			}
		}
	}

	return found
}

// sidebarBlock is a block the panel drew inside its own pane that the picker can land on.
type sidebarBlock struct {
	stop stop
	rect panes.Rect
}

// findSidebarBlocks draws the panel once with its blocks marked and reads where each one landed.
func (m model) findSidebarBlocks() []sidebarBlock {
	marked := m
	marked.markingZones = true

	zones := scanZones(marked.drawInset(marked.insideView()))
	stops := m.listStops()
	height := m.height

	var blocks []sidebarBlock

	details, hasDetails := zones[zoneDetails]
	if hasDetails && slices.Contains(stops, stopRecord) {
		blocks = append(blocks, sidebarBlock{stop: stopRecord, rect: details})
		height = details.Top
	}

	list := panes.Rect{
		Left:   0,
		Top:    0,
		Width:  m.width,
		Height: height,
	}
	column, hasColumn := zones[zoneColumn]
	if hasColumn {
		if !m.frame.IsSidebarOnRight() {
			list.Width = column.Left - dividerCols
		} else {
			list.Left = column.Left + column.Width + dividerCols
			list.Width = m.width - list.Left
		}
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
