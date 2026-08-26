package layout

type Region int

const (
	RegionNone Region = iota
	RegionSidebar
	RegionStage
	RegionInfo
)

type Tmux interface {
	IsPaneAlive(pane string) bool
	GetPaneWidth(pane string) int
	GetPaneHeight(pane string) int
	SplitRight(anchorPane, dir, size string) string
	SplitLeft(anchorPane, dir, size string) string
	SplitBelow(anchorPane, dir, size string) string
	SplitAbove(anchorPane, dir, size string) string
	ResizePane(pane, width string)
	SwapPanes(src, dst string)
	RespawnPane(pane, dir string, argv ...string)
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

	Wraps bool

	Render func(Target) ([]string, bool)
}
