# How it works

Weave has two programs: the service (server) and the panel (client).

The `weave serve` command starts the service. It owns the datbase connection, API, MCP, and runs the background loops that watch every agent. It is long-lived. Closing your terminal does not stop it, nor does stopping the panel.

The `weave panel` command starts the terminal interface. It is an ordinary client.

The panel starts the service if nothing is answering automatically, then talks to it over HTTP.

## Tmux

A tmux server is a long-lived process that owns sessions, windows, and panes, and you reach it through a unix socket. Weave runs one.

Your own server is where you work. Weave's server, named by `WEAVE_TMUX_SOCKET` and defaulting to `weave`, holds one session called the "manager" session, which the panel runs inside.

The panel reaches you through a popup. Pressing the weave keybinding runs a tmux popup on _your_ server whose command _attaches to the manager session_ on weave's server. Hiding the panel detaches that client. This leaves the session and every agent running in the background.

On the agents list, focus moves between the sidebar and the agent's pane on the stage with `C-a`. Both arms of the binding name the pane they move to, so a split you opened yourself is never what the key lands on. That one is bound outside tmux's prefix table on weave's own server, because a pane running an agent hands every key it gets to that agent, so the panel's own bindings never see one. A binding like that is unconditional for as long as it stands, so weave only stands it up while the agents list is showing and gives the key back on every other screen, where there is no agent's pane to move to anyway. It is the only key weave takes that way, and the agent on the stage loses it while the list is up, so `[panel] focus_key` in your config renames it or, left empty, binds nothing. A press with nowhere to go back to does nothing. Your own tmux server never sees this binding, but it does see the key first: if `C-a` is your prefix, weave's binding is never reached and you want another key here.

## Weave's API

Every call into weave goes through an API. The panel calls them as a ConnectRPC client.

For instance, telling weave to archive an agent is a keypress in the manager panel, which hits Weave's server, which archives the agent.

The `weave rpc` subcommand is a wrapper around those APIs, with one subcommand per RPC method. The MCP tools are generated from the same list.

Every method says who reaches it, in a `surfaces` annotation on the proto: whether `weave rpc` gets a command for it, and whether the MCP server offers it as a tool. Most methods are on both. A few are on neither and belong to the panel alone, such as emptying the archive, and a few are a command but not a tool, such as connecting a forge with a token a model should never hold. A surface that is off carries the reason, and a lint check refuses a method that says nothing.

The practical consequence of this, is that adding an endpoint gives you a CLI command and an MCP tool for free, every time, and taking one away is a deliberate line rather than an omission.

## The server architecture

Weave's architecture is built in a series of layers.

The first is the ConnectRPC handlers, the MCP server, and the CLI. These layers parse, validate, and translate. The interceptors in `internal/interceptors` are cross-cutting rules that do validation and payload manipulation logic.

The next layer, services, are `internal/agents` and `internal/rpc/*`. These indicate which RPCs trigger which logic. The business logic lives here.

The stores, which hold data, are in `internal/store` and the packages beneath it, one per domain. This package also holds database transactions.

Separately, adapters are in `internal/tmux`, `internal/harness`, and `internal/scripts`. Each one is a seam in front of something weave does not own. For instance, the `harness` package is where the boundary between different CLI agents (Claude vs. Codex) is kept.

## Postgres

State for weave is kept in Postgres. Every agent, workflow, schedule, and message is a row, and anything that must survive a restart is written there.

In order to reconcile state into tmux from the database, the server runs refresh (polling) loops.

The reconcile loop runs every two seconds. This loop is responsible for reconciling the state of the agent with the view in the manager panel. For instance, the agent's "thinking" state is determined based on the presence of text in the render window that looks like thinking text, not based on stop, start or other hooks, which are unreliable.

The schedule reconciler loop runs every ten seconds. This makes the job queue's registrations of work match the schedules table. The queue is managed by River, a Go package.

The mail delivery loop runs every two seconds. This finds mail (agent-to-agent messages) that the recipient hasn't read yet, and makes sure it's deliered. This handles instances where an agent has stopped and would otherwise miss messages, and ensures that all agent-to-agent communications are eventually delievered. This makes multi-agent orchestration patterns possible.

Finally, the transcript sync runs every fifteen seconds. It folds each agent's new conversation text into the search index, so that you and other agents can search previous conversations in Postgres more easily.

## End to End

Let's walk through one behavior, end-to-end.

Imagine you press the archive key. The panel's model turns that keypress into a command, which calls the `ArchiveAgents` method on the client. Nothing about the panel's own state changes yet.

The request arrives at the service and passes the interceptors, which log it, attach caller info, and validate it against the constraints declared in the protocol buffer file.

The handler in `internal/rpc/agent` then translates that request into a call on the service in `internal/agents`, which applies business logic rules: the agents must exist, and archiving them means stopping their panes as well as marking the rows.

The store then writes the rows, inside one transaction if more than one domain is involved. The service also kills the panes through the tmux adapter.

The panel learns about it on its next refresh, up to a second later, because it re-lists rather than being told. If the call failed, the error is reported on the panel's screen and nothing was written.

The process is similar for an agent doing the same thing: the call may come over MCP, but eventually it still reaches the same validators, services logic, and database transaction code.
