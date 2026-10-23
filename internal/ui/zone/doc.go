// Package zone marks where the panel renders a block or a row with escape codes a terminal doesn't
// show, and reads those marks back from a frame.
//
// The root, the diff view, the search line and the logs and review screens mark their blocks and
// rows with it. The root reads the marks back to send a click to the block or row under it, and to
// find the blocks copy mode selects in. The package imports nothing under internal/ui.
package zone
