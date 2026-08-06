// Package layout owns the tmux panes the panel lays out around itself and what is drawn in
// them. The nav region is the panel's own pane, the one it was started in and the only one it
// does not create. The main region is the stage beside it, and the bottom region hangs under
// the main one. A screen that wants something on the frame names a region and hands over
// content rather than driving tmux itself.
package layout

// Region is one of the frame's three regions, or none for the zero value.
type Region int

const (
	RegionNone Region = iota
	RegionNav
	RegionMain
	RegionBottom
)

// Tmux is the pane half of the panel's tmux client.
type Tmux interface {
	IsPaneAlive(pane string) bool
	GetPaneWidth(pane string) int
	SplitRight(anchorPane, dir string, percent int) string
	SplitBelow(anchorPane, dir string, percent int) string
	ResizePane(pane string, percent int)
	SwapPanes(src, dst string)
	RespawnPane(pane, dir string, argv ...string)
	GetSiblingPanes(pane string) []string
	BreakPane(pane string)
	KillPane(pane string)
	GetPaneOwner(pane string) string
}

// Target is the pane a piece of content is about to be drawn into.
type Target struct {
	Region Region
	// Width is the pane's columns, filled in only for content that wraps to it.
	Width int
}

// Content is something to draw in a region. Rendering is the caller's, since what a document
// looks like is the panel's business rather than the frame's.
type Content struct {
	// Key identifies what is drawn so it is not drawn again over itself, and an empty key
	// matches nothing.
	Key string
	// Wraps says the content is laid out to its pane's width, so a pane that has changed
	// width has to be drawn again.
	Wraps bool
	// Render is the command that fills the pane, or false when the content could not be
	// prepared.
	Render func(Target) ([]string, bool)
}
