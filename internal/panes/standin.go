package panes

type CopyStandIn struct {
	Pane        string
	Binary      string
	BorderStyle string
	Rows        []string
	Divider     int
	Rule        int
	Focus       Rect
	Row         int
	Col         int
	Keys        []string
}
