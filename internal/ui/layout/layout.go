package layout

import (
	"komrad/internal/panes"

	"github.com/google/uuid"
)

type region int

const (
	regionNone region = iota
	RegionSidebar
	RegionStage
	RegionInfo
)

type Tmux interface {
	MovePanes(moves []panes.Move)
	IsPaneAlive(pane string) bool
	GetPaneWidth(pane string) int
	GetWindowWidth(pane string) int
	GetPaneHeight(pane string) int
	SplitRight(anchorPane, dir, size string) string
	SplitBelow(anchorPane, dir, size string) string
	SplitAbove(anchorPane, dir, size string) string
	ResizePane(pane, width string)
	GetPaneFocus(pane string) (focused, zoomed bool)
	GetSiblingPanes(pane string) []string
	BreakPane(pane string)
	KillPane(pane string)
	GetPaneOwner(pane string) uuid.UUID
}

type Target struct {
	Region region
	Width  int
	Height int
}

type Content struct {
	Key string
	// Dir is the directory that roots the pane.
	Dir    string
	Wraps  bool
	Render func(Target) ([]string, bool)
}

const unmeasuredWidth = 30

// LineWidth is the column a line of prose wraps at, in a pane of the width given.
func LineWidth(pane int) int {
	if pane <= 0 {
		return unmeasuredWidth
	}

	return pane
}
