// Package history remembers where somebody has been and lets them walk back and forward through
// it, the way a browser's back and forward buttons do.
//
// It is a list of entries and a cursor. Stepping only moves the cursor. The list changes when a
// caller arrives somewhere by choice, which drops everything after the cursor, removes any
// earlier entry for the same place, and appends.
//
// Dropping keeps stepping back honest: the list is then the path actually taken to the current
// entry rather than a record of everywhere the cursor has sat. Removing the earlier entry is
// what makes the keypress worth pressing: without it, bouncing between two places by hand
// leaves a list alternating between them, and three steps back reach only two places.
//
// Arriving where you already are is ignored rather than treated as a move, so a caller that walks
// back and then acts on what it found keeps its forward entries. The cost is that such an entry
// keeps its place rather than moving to the end.
//
// The entries are whatever the caller wants to remember, so long as they can be compared. A
// caller whose entry has gone away removes it.
package history
