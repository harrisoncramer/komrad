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

Everything else in the package is one of those three for one screen.

## What the panel holds

The state divides into four kinds, and knowing which is which is most of understanding this
package.

The listing is what the last refresh brought back, the agents and workflows. It belongs to
the panel rather than to any one screen, because the schedule form and the archive both draw
the workflows the agents list happens to have fetched.

Per-screen state is the rest of what a screen draws: its cursor, and whatever it has loaded
that nothing else reads. treeState, detailsState, scheduleState, archiveState, searchState,
actionState.

Shared concerns are the things that sit across screens on purpose. The filter narrows
whichever list is on screen, and there is one of it rather than one per screen, because only
the list being looked at can be narrowed. The details under that list are shared for the same
reason: one cursor and one focus, whichever list is above them, with each screen saying only
what its own hovered row holds. The form owns the keys while it is up. The info region is
what is open in the pane below, which stays put as the user moves between lists above it. The
frame is the panel's own tmux layout.

Panel chrome is the rest: the banner over a refused write, the errors that replace the panel,
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
a refusal the banner goes up over the screen it happened on and nothing else moves, which is
what makes a refused delete different from a keypress that never registered: the row is still
there either way.

The one thing the panel does directly is its own layout. Splitting, swapping, and zooming
panes goes to tmux without a round trip through the service, because how the panel arranges
its own window is nobody else's business.

## The files

Screens, each holding its own rows, keys, and view: agents.go, with tree.go for the rows and
the cursor, workflows.go for the keys that act on a folder header, and selection.go for the
multi-select. Then archive.go, schedules.go, search.go, and actions.go.

Shared concerns: api.go, details.go, filter.go, forms.go, help.go, info.go, logs.go,
refresh.go, keys.go, clipboard.go, report.go, convert.go.

Presentation, holding no panel state at all: content.go, list.go, markdown.go, palette.go,
render.go, snippet.go, style.go.

The frame is its own package, internal/ui/layout, which owns the panes the panel lays out
around itself and is the only thing here that talks to tmux.

## Why the errors live in another package

internal/errui holds the failures that replace the panel, and the banner that reports a
refused write without replacing it.

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

Enforced by depguard, listed in .golangci.yaml. No tmux, no harness, no actions, no service
package, no database. The panel takes what it needs of tmux as a dependency so the option
names stay in the adapter, speaks to the service over the API like any other client, and
holds no connection of its own.
