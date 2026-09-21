# The panel

This is the terminal interface: one bubbletea program, drawn inside a tmux popup, that holds
no database connection and knows nothing the service has not told it. It is the largest
package in the tree, so this is the map of it. For how the panel fits with the service and
the two tmux servers, read docs/concepts.md first.

## The three ends of the loop

bubbletea is a loop over three things, and this package is organised around them.

model.go holds the state. One struct, with a field per screen for the rows it draws and where
its cursor sits, plus the concerns the screens share.

update.go takes messages in and routes each one to whatever owns it. A keypress goes to the
screen that currently has the keys, a finished listing goes to the refresh, a window resize
is recorded and then falls through, since an open form lays itself out from the same message.

view.go writes strings out. It draws the takeover screens first, then any chrome, then the
active screen.

screens.go is what the three of them dispatch through. A screen is a type implementing
screenModel, which says what takes its keys and what draws it, plus whichever of the facets it
draws: detailed for a record under its list, filtered for a search over it, tabbed for a strip
over it, columned for a block beside it, and staged for a row it puts on the stage. m.active()
is the switch from the screen enum to that type, and it carries no default, so the exhaustive
check refuses a screen with no arm. A screen that has moved, which the schedules and the
worktrees have, holds its own state in a type of its own; its handlers take a host, which is
what a screen may ask of the root, and a panelShared, which is what it may read of it, and
write nothing else. A screen that has not moved is an empty type whose methods call the
root's, which is the seam each screen's state moves behind one at a time. Drawing still goes
through the root's chrome on every screen, moved or not; screenlist.go is the list machinery
and the drawing bridge a moved list screen uses, beside list.go's until the last list screen
has moved.

Everything else in the package is one of those three for one screen.

## What the panel holds

The state divides into four kinds, and knowing which is which is most of understanding this
package.

The listing is what the last refresh brought back, the agents and workflows. It belongs to
the panel rather than to any one screen, because the schedule form and the archive both draw
the workflows the agents list happens to have fetched.

Per-screen state is the rest of what a screen draws: its cursor, and whatever it has loaded
that nothing else reads. treeState, schedulesScreen, archiveState, searchState, reviewState,
worktreesScreen.

Shared concerns are the things that sit across screens on purpose. The column is the block
drawn beside whichever list is up, and there is one of it rather than one per screen, because
only one block is ever on screen: where the reader has scrolled in it, and which row they
scrolled it on. The filter narrows whichever list is on screen, and there is one of it rather
than one per screen, because only the list being looked at can be narrowed. The details under
that list are shared for the same reason: one cursor and one focus, whichever list is above
them, with each screen saying only what its own hovered row holds. The form owns the keys
while it is up, and is drawn in that column rather than being a screen of its own, so closing
it lands back on the list it went up beside. The info region is what is open in the pane
below, which stays put as the user moves between lists above it. The frame is the panel's own
tmux layout.

Panel chrome is the rest: the messages a refused write is reported as, the errors that replace the panel,
the keybindings, the dimensions of its own pane.

## How data arrives

The panel polls. Every second a refresh fires, lists agents, workflows, and schedules
concurrently, and hands the results back as one message. Nothing pushes to the panel and
nothing about an agent is discovered here: the service watches the panes and writes what it
saw, and the panel reads rows like any other client.

Two guards keep that from fighting with what the user is doing.

Each write bumps an epoch, and a listing carries the epoch it was requested under. A pass
that read the store before a write landed is dropped whole rather than putting the write's
effect back on screen.

While any write is in flight the periodic refresh stands down, since it would read rows the
write is about to replace, and a write re-lists as it lands anyway.

Cursors are read off the trees still on screen before new rows are applied, then placed again
by the row they were on rather than the index that row happened to be drawn at. Either
listing can add or drop rows above a cursor.

## How a keypress becomes a change

The user presses a key. update.go routes it to the screen holding the keys, which turns it
into a command that calls the service through api.go. Nothing on screen changes yet.

The command runs off the render loop and comes back as a message. On success the panel
re-lists, so what appears is what the service stored rather than what the panel assumed. On
a refusal the message goes into the region under the record and nothing else moves, which is
what makes a refused delete different from a keypress that never registered: the row is still
there either way.

The one thing the panel does directly is its own layout. Splitting, swapping, and zooming
panes goes to tmux without a round trip through the service, because how the panel arranges
its own window is nobody else's business.

## The block beside the list

Every screen draws a list on the panel's own side and, beside it, one block: the diff being
reviewed, the transcript of a search hit, an archived agent's notes, a schedule's record, a
worktree directory's entries, the folder context or the reason a row has no pane, or a form
somebody is filling in.

column.go owns all of it. A screen names a columnSpec through the columned facet and the
machinery does the rest: columnGeometry divides the window and refuses one too narrow to hold
both halves, columnSplit takes off whatever stands over the block, and joinColumns squares each
half off and puts the sidebar on the side the frame wants it. Whether the panel is drawing a
block is the same question as whether it takes the whole window from the panes beside it, so
reconcileSolo answers both at once and every way onto a screen and into a form runs through it.

Two things are worth knowing before changing any of it. Anything the block renders that asks
paneWidth reaches back into the geometry that is still deciding the width, which is why the
widths and the row budget are two functions, and why a screen that has to measure its own
sidebar to decide is handed the answer through m.paneMeasure rather than asked. And a block
that falls short of its share lets the other half slide under it, which is what fitColumn is
for.

The exception is the agents list, whose block is only drawn for a row with no pane of its own.
A running agent's own terminal is what stands there, so that screen's columnSpec says when it
holds the window and when it gives it up.

## The files

Screens, each a type behind screens.go holding its own rows, keys, and view: agents.go, with
tree.go for the rows and the cursor, stage.go for the pane it puts beside the list, workflows.go
for the keys that act on a folder header, and selection.go for the multi-select. Then
archive.go, schedules.go, search.go, and worktreescreen.go, with worktrees.go and
worktreerows.go for its rows. The review screen is review.go and the review*.go files beside
it, split into the diff on screen, the threads pinned to it, and the pass over the work; the
rows it draws come from internal/ui/diffview. Each screen's forms sit beside it in a file of
their own: agentforms.go, workflowforms.go, scheduleforms.go, worktreeforms.go and
archiveforms.go, with forms.go holding only the machinery they share. Each screen's keys are
an actionSpec in a file of their own, agentsactions.go and the rest, except search, whose keys
sit in search.go; the four lists draw through one listSpec in list.go.

Shared concerns: api.go, actions.go, column.go, details.go, document.go, filter.go, forms.go,
help.go, info.go, logs.go, refresh.go, keys.go, clipboard.go, report.go, convert.go.

Presentation that still knows what it is drawing: content.go, list.go, markdown.go, style.go.
style.go is the marks a row wears for an agent's state, and what is left in list.go and
document.go is the model methods that measure a pane before handing the drawing on.

The frame is its own package, internal/ui/layout, which owns the panes the panel lays out
around itself and is the only thing here that talks to tmux.

The records are another, internal/ui/panel: an agent, a workflow and a schedule as the panel
keeps them, which is the fields the wire carries and nothing more. convert.go reads each proto
into one of them once, and nothing here imports the store's rows. depguard denies the panel the
store and the handlers' helpers so the panel binary links neither pgx nor river.

## The drawing primitives

internal/ui/paint is the colours, the styles, the markdown rendering, the arithmetic that fits
a list into a pane, and the markers a search hit carries. Nothing in it takes a model and
nothing in it reads a store, so what a screen draws stays the screen's business and how it is
drawn is paint's.

The rule is enforced rather than described: depguard denies paint the store, the database, the
protos, and the panel itself, listed in .golangci.yaml beside the panel's own rule.

Nothing names a colour directly. Each piece of the panel names a paint.Tone, the part it
plays, and paint.Panel, the one palette the panel draws in, turns that into a colour. So
retuning what an accent or a danger looks like is one edit rather than a sweep.

Colour says nothing about where the keys are. Greying every region but the one holding them
was tried and taken out, because the panel carries meaning in colour everywhere and flattening
a region destroyed what it was saying. What says where the keys are is the row cursor, which
blinks on the half holding them and stands still on the other, and the legend row, which lists
the keys of whatever is being typed at. The review screen has neither, so which of its two
halves holds the keys is not on the screen at all. A better signal is still to be worked out.

An agent's own terminal is another process drawing in its own colours, so no palette reaches
it.

The one contract paint cannot import is the pair of markers the transcript store wraps a
search hit in. It declares them itself and a test in the agent handler pins the two
together, so a change to either is a failing test rather than a silent mismatch.

## Why the errors live in another package

internal/errui holds the failures that replace the panel, and the readers that reduce a service
error to the reason and the trace the panel reports.

It is a separate package for two reasons. The first is that a screen shown instead of the
panel cannot depend on the panel: a config that will not parse means the keybindings were
never read, and a service that will not start means there is no listing to draw. Those
screens are rendered from a model that was never really built, so they hold presentation and
nothing about the panel's state.

The second is that the panel is not the only thing that reports these. The doctor's findings
are drawn on the startup screen, and errui.Finding is a restatement of doctor.Finding rather
than an import of it, so drawing a screen does not pull the checks and their database handles
in behind it. internal/cli maps one to the other at the boundary.

## What this package may not import

Enforced by depguard, listed in .golangci.yaml. No tmux, no harness, no scripts, no service
package, no database. The panel takes what it needs of tmux as a dependency so the option
names stay in the adapter, speaks to the service over the API like any other client, and
holds no connection of its own.
