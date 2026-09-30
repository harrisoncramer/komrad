package ui

import (
	"regexp"
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

				starts[name] = opened{left: col, top: row, width: width}

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

			found[name] = panes.Rect{Pane: string(name), Left: start.left, Top: start.top, Width: start.width, Height: bottom - start.top + 1}
		}
	}

	return found
}
