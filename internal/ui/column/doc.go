// Package column describes how the panel divides its window when a screen renders a block beside
// its own list, such as a document or a log beside the list of them.
//
// A screen hands the root a spec for its block, with the functions that render it bound to the
// screen's own state. The root reads the spec and divides the window between the list and the
// block. The package holds those two shapes and the widths they share.
//
// The root, the screen package and the screen domains import column. It imports only the panel
// package, and never the root or a domain.
package column
