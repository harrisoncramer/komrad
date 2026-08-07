# How it works

Weave is a service and a panel, and they are separate programs.

The `weave serve` command starts the service. It owns the datbase connection, API, MCP,
runs the background loops that watch every agent. It is long-lived. Closing your terminal does not stop it,
nor does stopping the panel.

The `weave panel` command starts the terminal interface. It is an ordinary client.

The panel starts the service if nothing is answering automatically, then talks to it over HTTP.

## Two tmux servers

A tmux server is a long-lived process that owns sessions, windows, and panes, and you reach it
through a unix socket. Weave runs one.

Your own server is where you work. Weave's server, named by `WEAVE_TMUX_SOCKET` and defaulting
to `weave`, holds one session called the "manager" session, which the panel runs inside, and one
window per agent.

The panel reaches you through a popup. Pressing the panel key runs a tmux popup on _your_ server
whose command _attaches to the manager session_ on weave's server. Hiding the panel detaches
that client. This leaves the session and every agent running in the background.

## Weave's API

Every action fired to weave goes through an API. The panel calls them as a ConnectRPC client.

The `weave rpc` subcommand is a wrapper around those APIs, with one subcommand per RPC method. The MCP tools are generated from the same list.

The practical consequence is that adding an endpoint gives you a CLI command and an MCP tool automatically.

## The layers

Weave is built in a series of layers. Each layer may call the one below it, but not past it.

The first is the ConnectRPC handlers, the MCP server, and the CLI. These layers parse, validate, and
translate. The interceptors in `internal/interceptors` are cross-cutting rules that do validation and payload
manipulation logic.

The next layer, services, are `internal/agents` and `internal/rpc/*`. These indicate which RPCs trigger
which logic. The business logic lives here.

The stores, which hold data, are in `internal/store` and the packages beneath it, one per domain.

The `internal/store` itself owns the pool and the transaction boundary.

Separately, adapters are in `internal/tmux`, `internal/harness`, and `internal/actions`. Each one is a seam
in front of something weave does not own. For instance, the `harness` package is where the boundary between
different CLI agents (Claude vs. Codex) is kept.

One known violation, recorded rather than hidden: `internal/ui/info.go` imports
`internal/tmux` directly instead of taking what it needs as a dependency the way the rest of
the panel does.

## Where state lives

State for weave is kept in Postgres. Every agent, workflow, schedule, and message is a row, and anything that must survive a restart is written there.

That state is passed to a tmux session, which holds in memory which panes exist, and which agent each belongs to. But panel memory is a cache. The panel re-lists over RPC every second and re-draws itself. The panel splits, swaps, and zooms panes for its own layout.

In order to reconcile state into tmux from the database, the server runs refresh (polling) loops.

The reconcile loop runs every two seconds. Rather than react to lifecycle events from
an agent CLI, which fire unreliably and miss endings such as an interrupt, it captures each
agent's visual pane itself. Meaning, the agent's "thinking" state is determined based on the presence
of text in the tmux render window that looks like thinking text -- not based on stop, start or other hooks.

This is because hooks can be dropped and are unreliable, in my experience.

The schedule reconcile loop runs every ten seconds. This makes the job queue's registrations of work match the schedules table. The queue is River.

The mail delivery loop runs every two seconds. It finds mail nobody has read and gets it in front of whoever it is for, including an agent that has stopped and would otherwise never look.

Finally, the transcript sync runs every fifteen seconds. It folds each agent's new conversation text into the search index.

## One action, end to end

Archiving an agent from the panel, as an example of the whole path.

You press the archive key. The panel's model turns that keypress into a command, which calls
`ArchiveAgents` on the agent service client. Nothing about the panel's own state changes yet.

The request arrives at the service and passes the interceptors, which log it, attach who is
asking, and validate it against the constraints declared in the proto.

The handler in `internal/rpc/agent` translates the request into a call on the service in
`internal/agents`, which applies the rules: the agents must exist, and archiving them means
stopping their panes as well as marking the rows.

The store writes the rows, inside one transaction if more than one domain is involved. The
service kills the panes through the tmux adapter.

The panel learns about it on its next refresh, up to a second later, because it re-lists rather
than being told. If the call failed, the error is reported on the panel's own screen and
nothing was written.

Every other front door takes the same path from the second step onward. A model calling
`archive_agents` and a shell running `weave rpc archive-agents` differ only in how the request
was composed.

## What is deliberately not here

Which agent CLI an agent runs is behind `internal/harness`. Nothing outside that package knows
what a command line looks like or how to read a frame.

What an action does is behind `internal/actions`. An action is an executable file with an
agent handed to it in the environment, and what it does with that is the user's business.

How the panel is drawn is `internal/ui`. It is the largest package in the tree, and splitting
it by screen is planned work rather than something this page describes as settled.
