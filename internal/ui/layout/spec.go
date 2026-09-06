package layout

import (
	"math"
	"strconv"

	"weave/config"
)

type spec struct {
	sidebarOnRight bool
	sidebarSize    string
	// sidebarShare is that same width as the number rather than the string tmux takes, since a
	// screen holding the whole window draws its own split and needs the columns.
	sidebarShare float64
	stageSize    string
	infoAbove    bool
	infoSize     string
}

// newSpec resolves a configured layout into the geometry the frame lays panes out at, falling
// back to the defaults for anything the config left out.
func newSpec(layout config.Layout) spec {
	list, info := layout.SidebarWidth, layout.InfoHeight
	if list <= 0 {
		list = config.DefaultSidebarWidth
	}
	if info <= 0 {
		info = config.DefaultInfoHeight
	}

	return spec{
		sidebarOnRight: layout.SidebarSide == config.SidebarSideRight,
		sidebarSize:    formatSize(list),
		sidebarShare:   list,
		stageSize:      formatComplement(list),
		infoAbove:      layout.InfoSide == config.InfoSideAbove,
		infoSize:       formatSize(info),
	}
}

func formatSize(size float64) string {
	if size >= 1 {
		return strconv.Itoa(int(size))
	}

	return strconv.Itoa(percent(size)) + "%"
}

func formatComplement(size float64) string {
	if size >= 1 {
		return "50%"
	}

	return strconv.Itoa(100-percent(size)) + "%"
}

func percent(share float64) int {
	return int(math.Round(share * 100))
}

// SidebarWidth is how many of a window's columns the sidebar takes, resolving a share of the
// window against the width it is actually being drawn at.
func (s spec) SidebarWidth(total int) int {
	if total <= 0 {
		return 0
	}

	if s.sidebarShare >= 1 {
		return min(int(s.sidebarShare), total)
	}

	return min(int(math.Round(s.sidebarShare*float64(total))), total)
}
