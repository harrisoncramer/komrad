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
// The status bar is the one cell that draws nothing rather than drawing greyed. A dim pipe says
// there is a process here and it is quiet, and an empty cell says there is no process at all, so
// a dormant agent reads as a name with no channel beside it. Blank carries that meaning only
// because every other cell is always drawn.
//
// The old sidebar did offset an agent's name from a workflow's, because agent rows carried a
// state icon that workflow rows did not. Here the status bar is in the shared gutter on every
// row, so that cause is gone and any offset left over would be decoration rather than
// consequence.
//
// # A narrow pane gives up columns, not names
//
// The columns drop from the right as the pane narrows, forge first and then review, decided by
// the pane alone so a list never changes shape because a row gained a mark. The repo column is
// never given up, since a sidebar too narrow for the rest still says whether the checkout under
// an agent is one you can work in. Nothing else is given up for width: not the focus caret, the
// mark gutter, the status bar or the marks after a name. A row too full for what is left gives
// up its tally and then its name, which is a row at a time rather than the whole list.
//
// # Scrolling belongs to the caller
//
// Rows hands back one drawn string per row and stops there. Which of them fit the pane, and
// which the list is scrolled to, is the caller's, since only the caller knows what else it has
// put above and below them. A caller measuring those rows applies the same correction the
// selection bar does, because an Octicon sits in the private use area and is measured as one
// column where the font draws two.
//
// # Zero values are the ordinary case
//
// Every enum in a Snapshot starts at what an unremarkable row would say: an agent, a clean
// checkout, no review, no pull request, idle. A test writing Row{} therefore gets the row it
// meant rather than a workflow, which is a mistake that stays invisible until somebody reorders
// the constants.
package view
