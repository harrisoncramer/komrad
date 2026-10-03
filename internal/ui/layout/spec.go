package layout

import (
	"math"
	"strconv"

	"komrad/config"
)

type spec struct {
	sidebarOnRight bool
	// sidebarCols is the width the sidebar is first laid out at, in columns.
	sidebarCols int
	infoAbove   bool
	infoSize    string

	// tile is whether a terminal is shown across the bottom of the window at all.
	tile bool
	// tileHeight is the share of the window's rows the tile takes, or a count of them from one up.
	tileHeight float64
	tileMin    int
	tileMax    int
}

// newSpec resolves a configured layout into the geometry the frame lays panes out at, falling
// back to the defaults for anything the config left out.
func newSpec(layout config.Layout) spec {
	cols, info := layout.SidebarWidth, layout.InfoHeight
	if cols <= 0 {
		cols = config.DefaultSidebarWidth
	}
	if info <= 0 {
		info = config.DefaultInfoHeight
	}

	tileHeight := layout.ReviewTerminalHeight
	if tileHeight <= 0 {
		tileHeight = config.DefaultReviewTerminalHeight
	}

	tileMin, tileMax := layout.ReviewTerminalMinRows, layout.ReviewTerminalMaxRows
	if tileMax <= 0 {
		tileMin, tileMax = config.DefaultReviewTerminalMinRows, config.DefaultReviewTerminalMaxRows
	}

	return spec{
		sidebarOnRight: layout.SidebarSide == config.SidebarSideRight,
		sidebarCols:    cols,
		infoAbove:      layout.InfoSide == config.InfoSideAbove,
		infoSize:       formatSize(info),
		tile:           layout.ReviewTerminal,
		tileHeight:     tileHeight,
		tileMin:        tileMin,
		tileMax:        tileMax,
	}
}

// stageSize is the share the stage is split off at, before the sidebar is resized to the columns
// it was configured for.
const stageSize = "50%"

func formatSize(size float64) string {
	if size >= 1 {
		return strconv.Itoa(int(size))
	}

	return strconv.Itoa(percent(size)) + "%"
}

func percent(share float64) int {
	return int(math.Round(share * 100))
}

// sidebarWidth is how many of a window's columns the sidebar starts at.
func (s spec) sidebarWidth(total int) int {
	if total <= 0 {
		return 0
	}

	return min(s.sidebarCols, total)
}

// tileRows is how many of a window's rows the tile takes, held between the configured floor and
// cap.
func (s spec) tileRows(total int) int {
	rows := int(s.tileHeight)
	if s.tileHeight < 1 {
		rows = int(math.Round(s.tileHeight * float64(total)))
	}

	return min(max(rows, s.tileMin), s.tileMax)
}
