package layout

import (
	"math"
	"strconv"

	"weave/config"
)

// Spec is where each region sits and how big it is, in the sizes tmux itself takes. It is
// resolved from the user's config once, so nothing further down has to know what an omitted
// key means or which of the two forms a size was written in.
type Spec struct {
	// sidebarOnRight puts the panel's own pane right of the stage rather than left of it.
	sidebarOnRight bool
	// sidebarSize is how wide the sidebar is, a column count or a percentage of the window.
	sidebarSize string
	// stageSize is how wide the stage is split off at. A sidebar sized in columns leaves no
	// remainder to work out, so the stage takes half the window and the resize that follows
	// settles it.
	stageSize string
	// infoAbove opens the info pane over it instead.
	infoAbove bool
	// infoSize is how tall that region opens, a row count or a percentage of the stage.
	infoSize string
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

// formatSize writes a configured size the way tmux reads one, a share as a percentage and
// anything at or above one as the count of cells it already is.
func formatSize(size float64) string {
	if size >= 1 {
		return strconv.Itoa(int(size))
	}

	return strconv.Itoa(percent(size)) + "%"
}

// formatComplement is what is left of the window once a share is taken out of it. A size in
// cells has no complement to state, so the rest of the window is split evenly and resized
// afterwards.
func formatComplement(size float64) string {
	if size >= 1 {
		return "50%"
	}

	return strconv.Itoa(100-percent(size)) + "%"
}

func percent(share float64) int {
	return int(math.Round(share * 100))
}
