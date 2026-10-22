package list

import (
	"komrad/internal/ui/legend"
	"komrad/internal/view"
)

// Params is a screen's list as the root renders it: its title and tally, its own legend, and the
// rows the window is filled from.
type Params struct {
	Title string
	Tally string
	// Help is the screen's own legend, rendered ahead of the bindings every list shares.
	Help legend.Help
	// Empty is what the list says over no rows, and blank on a list that renders its window anyway.
	Empty string
	// Welcome, while set, is centred in place of the list.
	Welcome string
	// Rows is how many rows the list holds.
	Rows     int
	Snapshot func(Pane) view.Snapshot
	// HidesKinds keeps the row of kinds off however much room the list has.
	HidesKinds bool
}
