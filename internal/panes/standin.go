package panes

// CopyStandIn is a stand-in for a pane in copy-mode: the pane it replaces, the text it prints,
// where its cursor starts, and the keys the pane is sent once it is gone.
type CopyStandIn struct {
	Pane string
	Text string
	Row  int
	Col  int
	Keys []string
}
