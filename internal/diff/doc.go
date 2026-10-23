// Package diff loads the patch of a snapshot from the commits since its branch left the base
// branch, and parses it into lines that carry their own numbers. The panel's diff view uses those
// numbers to turn a cursor on the diff back into a place in a file.
package diff
