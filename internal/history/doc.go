// Package history remembers the places somebody has been and lets them walk back and forward
// through them, the way a browser's back and forward buttons do.
//
// It is a list of entries and a cursor saying which one is current. Stepping does not change the
// list, it only moves the cursor, so walking back and forward is free and reversible. The list
// changes only when a caller says it has arrived somewhere by its own choice.
//
// Arriving does three things in order. Everything after the cursor is dropped, since a forward
// list only means the places you have just come back from. An existing entry for the same place
// is removed, so a place appears once rather than once per visit. Then the entry is appended and
// the cursor moves to it.
//
// That middle step is what makes stepping back worth the keypress. Without it, bouncing between
// two places by hand leaves a list that alternates between them, and three steps back land on
// two distinct places. With it the list is a recency ordering rather than a log, and every step
// back reaches somewhere new.
//
// Arriving where you already are is ignored rather than treated as a move. A caller that walks
// back and then acts on what it found keeps its forward list, which it would otherwise throw
// away by acting. The cost is that such an entry keeps its place rather than moving to the end,
// so the recency ordering holds for every entry except the one under the cursor.
//
// The entries are whatever the caller wants to remember, so long as they can be compared. The
// package holds no opinion about what makes one valid, which is why a caller whose entry has
// gone away removes it rather than teaching this package to recognise the dead.
package history
