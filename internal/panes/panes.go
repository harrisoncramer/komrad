// Package panes is the handful of pane moves the panel asks of tmux, named as values so that
// the panel can decide a whole change before any of it is sent.
//
// It sits between the two because neither may name the other: the panel may not import the
// tmux adapter, and the adapter has no business importing the panel.
package panes

// Kind is which command a move stands for.
type Kind int

const (
	KindZoom Kind = iota
	KindSwap
	KindRespawn
)

// Move is one pane move: what to do, and to which panes.
type Move struct {
	Kind Kind
	// Pane is the pane the move acts on, and for a swap the one moving into Other's place.
	Pane string
	// Other is the second pane of a swap, and empty otherwise.
	Other string
	// Dir is where a respawned pane is rooted, and empty otherwise.
	Dir string
	// Argv is what a respawned pane runs, empty meaning the default shell on a blank pane.
	Argv []string
	// Zoomed is the zoom a KindZoom move leaves the pane on.
	Zoomed bool
}

// Zoom is the move that gives a pane the whole window, or hands the others their share back.
func Zoom(pane string, zoomed bool) Move {
	return Move{
		Kind:   KindZoom,
		Pane:   pane,
		Zoomed: zoomed,
	}
}

// Swap is the move that exchanges two panes' positions.
func Swap(src, dst string) Move {
	return Move{
		Kind:  KindSwap,
		Pane:  src,
		Other: dst,
	}
}

// Respawn is the move that replaces a pane's process, blanking it when argv is empty.
func Respawn(pane, dir string, argv ...string) Move {
	return Move{
		Kind: KindRespawn,
		Pane: pane,
		Dir:  dir,
		Argv: argv,
	}
}
