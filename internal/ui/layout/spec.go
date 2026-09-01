package layout

import (
	"math"
	"strconv"

	"weave/config"
)

type Spec struct {
	sidebarOnRight bool
	sidebarSize    string
	stageSize      string
	infoAbove      bool
	infoSize       string
}

// NewSpec resolves a configured layout into the geometry the frame lays panes out at, falling
// back to the defaults for anything the config left out.
func NewSpec(layout config.Layout) Spec {
	list, info := layout.SidebarWidth, layout.InfoHeight
	if list <= 0 {
		list = config.DefaultSidebarWidth
	}
	if info <= 0 {
		info = config.DefaultInfoHeight
	}

	return Spec{
		sidebarOnRight: layout.SidebarSide == config.SidebarSideRight,
		sidebarSize:    formatSize(list),
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
