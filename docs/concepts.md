# How it works

Weave has two programs: the service (server) and the panel (client).

The `weave serve` command starts the service. It owns the datbase connection, API, MCP, and runs the background loops that watch every agent. It is long-lived. Closing your terminal does not stop it, nor does stopping the panel.

The `weave panel` command starts the terminal interface. It is an ordinary client.

The panel starts the service if nothing is answering automatically, then talks to it over HTTP.

## Tmux

A tmux server is a long-lived process that owns sessions, windows, and panes, and you reach it through a unix socket. Weave runs one.

Your own server is where you work. Weave's server, named by `WEAVE_TMUX_SOCKET` and defaulting to `weave`, holds one session called the "manager" session, which the panel runs inside.

The panel reaches you through a popup. Pressing the weave keybinding runs a tmux popup on _your_ server whose command _attaches to the manager session_ on weave's server. Hiding the panel detaches that client. This leaves the session and every agent running in the background.

## Weave's API

Every action fired to weave goes through an API. The panel calls them as a ConnectRPC client.

For instance, telling weave to archive an agent is a keypress in the manager panel, which hits Weave's server, which archives the agent.

The `weave rpc` subcommand is a wrapper around those APIs, with one subcommand per RPC method. The MCP tools are generated from the same list.

The practical consequence of this, is that adding an endpoint gives you a CLI command and an MCP tool for free, every time.

## The server architecture

Weave's architecture is built in a series of layers.

The first is the ConnectRPC handlers, the MCP server, and the CLI. These layers parse, validate, and translate. The interceptors in `internal/interceptors` are cross-cutting rules that do validation and payload manipulation logic.

The next layer, services, are `internal/agents` and `internal/rpc/*`. These indicate which RPCs trigger which logic. The business logic lives here.

The stores, which hold data, are in `internal/store` and the packages beneath it, one per domain. This package also holds database transactions.

Separately, adapters are in `internal/tmux`, `internal/harness`, and `internal/actions`. Each one is a seam in front of something weave does not own. For instance, the `harness` package is where the boundary between different CLI agents (Claude vs. Codex) is kept.

## Postgres

State for weave is kept in Postgres. Every agent, workflow, schedule, and message is a row, and anything that must survive a restart is written there.

That state is passed to a tmux session, which holds in memory which panes exist, and which agent each belongs to. But panel memory is a cache. The panel re-lists over RPC every second and re-draws itself. The panel splits, swaps, and zooms panes for its own layout.

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

The process is similar for an agent taking the same action: the call may come over MCP, but eventually it still reaches the same validators, services logic, and database transaction code.
