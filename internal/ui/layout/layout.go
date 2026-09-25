// Package layout owns the panes the panel is drawn across, turning the regions it asks for
// into splits on komrad's tmux server.
//
// The panel is one pane among several rather than a whole screen. The sidebar it draws
// itself, and beside it sit the stage that pages a file or a diff, the info pane, and the
// guest pane an attached agent takes over. This package decides where those go and which of
// them are open, so the screens ask for a region rather than for a pane id.
package layout

import "komrad/internal/panes"

type Region int

const (
	RegionNone Region = iota
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
	SplitLeft(anchorPane, dir, size string) string
	SplitBelow(anchorPane, dir, size string) string
	SplitAbove(anchorPane, dir, size string) string
	ResizePane(pane, width string)
	GetPaneFocus(pane string) (focused, zoomed bool)
	GetSiblingPanes(pane string) []string
	BreakPane(pane string)
	KillPane(pane string)
	GetPaneOwner(pane string) string
}

type Target struct {
	Region Region
	Width  int
	Height int
}

type Content struct {
	Key string
	// Dir is where the pane is rooted.
	Dir    string
	Wraps  bool
	Render func(Target) ([]string, bool)
}
