// Package view is the agents list drawn in three steps: a snapshot of what the panel found, a
// pure preparation of that snapshot into resolved glyphs and tones, and a pure rendering of
// those into a string.
//
// Nothing here holds a client, a clock, a terminal or a context. A Snapshot is plain data the
// panel gathers; Prepare makes every decision about which glyph, which tone and which weight a
// row wears; Render spells those decisions out with the palette of the region it is drawn in.
// The split is what lets a change of logic fail a readable assertion and a change of palette
// move only a golden file.
//
// # Every name on one column
//
// A row is a fixed sequence of cells: focus, mark gutter, repo, review, forge, status bar, name,
// then trailing marks. Every cell is always drawn and holds its column whether or not it has
// anything to say, so every row opens its name at the same place whatever kind it is. A workflow
// is told from an agent by weight and by its bar, and a child by italic and by the corner inside
// its name cell, never by position.
//
// The old sidebar did offset an agent's name from a workflow's, because agent rows carried a
// state icon that workflow rows did not. Here the status bar is in the shared gutter on every
// row, so that cause is gone and any offset left over would be decoration rather than
// consequence.
//
// # Zero values are the ordinary case
//
// Every enum in a Snapshot starts at what an unremarkable row would say: an agent, a clean
// checkout, no review, no pull request, idle. A test writing Row{} therefore gets the row it
// meant rather than a workflow, which is a mistake that stays invisible until somebody reorders
// the constants.
package view
