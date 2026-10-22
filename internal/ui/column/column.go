// Package column holds how the panel divides its window while it renders a block beside its own list.
package column

// Split is how the window is divided while the panel is rendering a block beside its own list: the
// block on one side, and the list, its record and its legend on the other.
type Split struct {
	Column  int
	Sidebar int
	Rows    int
	// On is whether the window is divided at all.
	On bool
}
