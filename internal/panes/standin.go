package panes

type CopyStandIn struct {
	Pane  string
	Text  string
	Row   int
	Col   int
	Block Rect
	Keys  []string
}
