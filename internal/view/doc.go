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
// # Agents indent under their workflow
//
// A row is a fixed sequence of cells: focus, mark gutter, repo, review, forge, status bar, name,
// then trailing marks. Every gutter cell is always drawn and holds its column whether or not it
// has anything to say, so the gutter is identical on every row whatever kind it is. Only the
// name is indented: a workflow opens at the gutter, an agent one step in, and a child one step
// further, on the corner it hangs off its parent by. Weight carries the same structure over
// again, bold for a workflow and italic for a child, so neither the indent nor the lettering is
// the only thing saying where a row sits.
//
// The status bar is the one cell that draws nothing rather than drawing greyed. A dim pipe says
// there is a process here and it is quiet, and an empty cell says there is no process at all, so
// a dormant agent reads as a name with no channel beside it. Blank carries that meaning only
// because every other cell is always drawn.
//
// The old sidebar did offset an agent's name from a workflow's, because agent rows carried a
// state icon that workflow rows did not. Here the status bar is in the shared gutter on every
// row, so that cause is gone. The indent above is therefore a decision rather than the leftover
// of one: it was asked for.
//
// An indent costs the name the columns it spends, so an agent has less room for its name than a
// workflow at the same pane, and the narrowing and the mark shedding both read the indent rather
// than the gutter alone.
//
// # A narrow pane gives up columns, not names
//
// The columns drop from the right as the pane narrows, forge first and then review, decided by
// the pane alone so a list never changes shape because a row gained a mark. The repo column is
// never given up, since a sidebar too narrow for the rest still says whether the checkout under
// an agent is one you can work in. Nothing else is given up for width: not the focus caret, the
// mark gutter or the status bar. A row too full for what is left gives up its tally and then its
// name, a row at a time rather than the whole list, and gives up its marks only where the marks
// alone would not fit the pane: the schedule first, then the pause, then the mail, and the
// failed run last.
//
// # A screen may give up a column its rows could never fill
//
// The narrowing above is about room. Dropped is about meaning: a column a screen gives up there
// is gone at every pane, because its rows will never reach that stage of the work. The archive is
// the case that asked for it, where an agent has been put away and its checkout, its review and
// its pull request are none of them looked at again, so a greyed glyph on each would be promising
// something that will never happen rather than saying not yet.
//
// A dropped column buys the name its room, since the gutter arithmetic reads the columns a list
// ended up with rather than the three it might have had.
//
// # Scrolling belongs to the caller
//
// Rows hands back one drawn string per row and stops there. Which of them fit the pane, and
// which the list is scrolled to, is the caller's, since only the caller knows what else it has
// put above and below them. A caller measuring those rows applies the same correction the
// selection bar does, because an Octicon sits in the private use area and is measured as one
// column where the font draws two.
//
// Window is that work already done, for a caller that has only to say how many lines it has
// left. It scrolls to the row the snapshot named, and a key naming no row at all leaves the
// list where a fresh one opens.
//
// # Zero values are the ordinary case
//
// Every enum in a Snapshot starts at what an unremarkable row would say: an agent, a clean
// checkout, no review, no pull request, idle. A test writing Row{} therefore gets the row it
// meant rather than a workflow, which is a mistake that stays invisible until somebody reorders
// the constants.
package view
