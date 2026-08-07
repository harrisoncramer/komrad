# How it works

This is the map. It covers what runs, where it runs, who is allowed to write what, and the
path a single action takes from a keypress to the database and back. Every package here has
a doc comment explaining itself; what this page adds is how they fit together.

## Two processes

Weave is a service and a panel, and they are separate programs.

`weave serve` is the service. It owns the Postgres connection pool, answers the API, serves
the MCP endpoint, and runs the background loops that watch every agent. It is long-lived.
Closing your terminal does not stop it, and neither does closing the panel.

`weave panel` is the terminal interface. It is an ordinary client of the service and holds no
database connection of its own. That is why it can be closed and reopened without stopping
anything, and why a panel that has crashed leaves every agent running.

The panel starts the service if nothing is answering, then talks to it over HTTP like any
other caller. The address defaults to 127.0.0.1:8787 and is one of the settings that can be
changed in your config.

## Two tmux servers

A tmux server is a long-lived process that owns sessions, windows, and panes, and you reach it
through a unix socket. Weave runs a second one.

Your own server is where you work. Weave's server, named by `WEAVE_TMUX_SOCKET` and defaulting
to `weave`, holds one session called the manager session, which the panel runs inside, and one
window per agent. Nothing in your normal tmux can see it, which is deliberate: you cannot
attach to weave's session by accident, and `tmux ls` stays yours.

The panel reaches you through a popup. Pressing the panel key runs a tmux popup on your server
whose command attaches to the manager session on weave's server. Hiding the panel detaches
that client, which leaves the session and every agent running, so reopening restores the exact
view.

That isolation cuts both ways, and `internal/tmux` is where it is handled. `Servers` presents
the pair as a single client and sends each call to whichever server can answer it. Sessions,
windows, and panes belong to weave's server. Global options and the working directory of the
client viewing the popup live only on yours, because that is where the key binding reads them.

## One API, several front doors

Every way into weave goes through the same endpoints.

The protos under `proto/proto` define the methods. `internal/rpcapi` enumerates them and reads
the annotations that say which are exposed to models. From that one list:

The panel calls them as a connect client, in `internal/ui/api.go`.

`weave rpc <method>` is generated from the same list, one subcommand per RPC.

The MCP tools are generated from the same list, and the MCP server calls the service over HTTP
rather than reaching into the handlers. A request from a model therefore passes through the
same validation and logging as everything else.

The agent CLI hooks, such as the stop hook that delivers mail, are clients too.

Actions are handed the address in their environment and call `weave rpc`.

The practical consequence is that adding an endpoint gives you a CLI command and an MCP tool
without writing either, and that there is no privileged path that skips the checks.

## The layers

Read top to bottom. Each layer may call the one below it and should not reach past it.

Transport is connect handlers, the MCP server, and the CLI. It parses, validates, and
translates, and it holds no rules of its own. The interceptors in `internal/interceptors`
belong here: they are the cross-cutting rules, written once rather than per handler.

Services are `internal/agents` and `internal/rpc/*`. This is where the rules live: what a
launch resolves to, when an archive is allowed, what happens to the agents inside a workflow
that is archived.

Stores are `internal/store` and the packages beneath it, one per domain. `internal/store`
itself owns the pool and the transaction boundary, which is why a write spanning two domains
runs through `InTx` and hands both stores the same handle.

Adapters are `internal/tmux`, `internal/harness`, and `internal/actions`. Each one is a seam
in front of something weave does not own. `harness` in particular is where everything true of
one agent CLI and not another is kept, so supporting a second CLI is a new package rather than
a change to the reconciler.

One known violation, recorded rather than hidden: `internal/ui/info.go` imports
`internal/tmux` directly instead of taking what it needs as a dependency the way the rest of
the panel does.

## Where state lives

Three places, and the distinction is the thing to keep straight.

Postgres is the truth. Every agent, workflow, schedule, and message is a row, and anything
that must survive a restart is written there.

tmux is reality. Which panes exist, and which agent each belongs to, is a fact about the
world rather than a row, and weave reads it rather than trusting its own record. Every agent's
pane is stamped with a pane-scoped option holding that agent's id, which is how a pane that
tmux has since reissued is told apart from the one weave started.

Panel memory is a cache. The panel re-lists over RPC every second and draws what it last
heard. It is never authoritative, and nothing else reads it.

Two things write tmux, and it is worth knowing which is which. The service opens and kills
agent panes, because that is part of an agent's lifecycle. The panel splits, swaps, and zooms
panes for its own layout, because how the panel arranges its own window is nobody else's
business.

## The loops

The service runs four, declared together at the top of `internal/server`. They do not call
each other. Each reads the world, compares it to what is stored, and corrects the difference,
so a wrong state cannot outlive one interval.

Reconcile, every two seconds, is the state model. Rather than react to lifecycle events from
an agent CLI, which fire unreliably and miss endings such as an interrupt, it captures each
agent's pane and asks that agent's harness what the frame implies. Reading ground truth every
pass is what makes the panel's states trustworthy. A thinking agent must be observed idle
three passes running before it settles, since a single capture can land on a redraw between
tool calls.

Schedule reconcile, every ten seconds, makes the job queue's registrations match the schedules
table. The queue is River, running on the same Postgres.

Mail delivery, every two seconds, finds mail nobody has read and gets it in front of whoever it
is for, including an agent that has stopped and would otherwise never look.

Transcript sync, every fifteen seconds, folds each agent's new conversation text into the
search index, reading only the appended tail rather than reparsing the file.

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
