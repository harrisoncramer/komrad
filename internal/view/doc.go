// Package view draws the panel's lists of rows, for the agents list with its workflows and tasks
// and for the worktrees, documents, schedules and search screens built on the same rows.
//
// Drawing takes three steps. The panel gathers a [Snapshot] of plain data. [Prepare] decides
// every glyph, tone and weight each row wears and returns a [List]. [Window] renders the part of
// that list the pane holds, scrolled to the focused row. Nothing here holds a client, a clock or a
// terminal, so a change of logic fails a readable assertion and a change of palette moves only a
// golden file.
//
// A row is a fixed run of cells. It holds focus, the columns its screen declares, the name,
// trailing marks, and the comment counts. Each screen names its own columns in
// [Snapshot.Columns], and a snapshot naming none gets the agents list's repo, review and forge
// columns. A blank cell has nothing to say, and every gutter cell keeps its width so the columns
// line up on every row. When a row is too wide for the pane, it gives up its tally, then its
// comment counts, then room in its name, and its marks last.
//
// In the agents list, a tree in grey hangs each task off its workflow and each agent off its task,
// and the task row stands for its writer. A name renders dim only when nothing runs behind the row
// any more, because its process stopped or it was archived. A hidden task stays bright and
// carries a mark after its name instead.
//
// Every enum in a [Row] starts at what an unremarkable agent would show, so a test writing Row{}
// gets an idle agent with a clean checkout.
package view
