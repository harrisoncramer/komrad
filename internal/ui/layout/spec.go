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
	// info is the configured info height, a share of the stage below one and rows from one up.
	info float64
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

	return spec{
		sidebarOnRight: layout.SidebarSide == config.SidebarSideRight,
		sidebarCols:    cols,
		infoAbove:      layout.InfoSide == config.InfoSideAbove,
		infoSize:       formatSize(info),
		info:           info,
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

// stageOverInfo is the size the stage is split back off an info pane of the given height at.
func (s spec) stageOverInfo(height int) string {
	if s.info < 1 {
		return strconv.Itoa(100-percent(s.info)) + "%"
	}

	return strconv.Itoa(max(height-int(s.info)-1, 1))
}

// sidebarWidth is how many of a window's columns the sidebar starts at.
func (s spec) sidebarWidth(total int) int {
	if total <= 0 {
		return 0
	}

	return min(s.sidebarCols, total)
}
