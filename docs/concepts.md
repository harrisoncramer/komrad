# How it works

Weave has two programs: the service (server) and the panel (client).

The `weave serve` command starts the service. It owns the database connection, API, MCP, and runs the background loops that watch every agent. It is long-lived. Closing your terminal does not stop it, nor does stopping the panel.

The `weave panel` command starts the terminal interface. It is an ordinary client.

The panel starts the service if nothing is answering automatically, then talks to it over HTTP.

## Tmux

A tmux server is a long-lived process that owns sessions, windows, and panes, and you reach it through a unix socket. Weave runs one.

Your own server is where you work. Weave's server, named by `WEAVE_TMUX_SOCKET` and defaulting to `weave`, holds one session called the "manager" session, which the panel runs inside.

The panel reaches you through a popup. Pressing the weave keybinding runs a tmux popup on _your_ server whose command _attaches to the manager session_ on weave's server. Hiding the panel detaches that client. This leaves the session and every agent running in the background.

On the agents list, focus moves between the sidebar and the agent's pane on the stage with `C-a`. Both arms of the binding name the pane they move to, so a split you opened yourself is never what the key lands on. That one is bound outside tmux's prefix table on weave's own server, because a pane running an agent hands every key it gets to that agent, so the panel's own bindings never see one. A binding like that is unconditional for as long as it stands, so weave only stands it up while the agents list is showing and gives the key back on every other screen, where there is no agent's pane to move to anyway. It is the only key weave takes that way, and the agent on the stage loses it while the list is up. The key is fixed rather than configured. A press with nowhere to go back to does nothing. Your own tmux server never sees this binding, but it does see the key first: if `C-a` is your prefix, weave's binding is never reached and you want another key here.

## Weave's API

Every call into weave goes through an API. The panel is one client of it.

For instance, telling weave to archive an agent is a keypress in the manager panel, which hits Weave's server, which archives the agent.

The `weave rpc` subcommand is a wrapper around those APIs, with one subcommand per RPC method. The MCP tools are generated from the same list.

Every method in the API says who reaches it: whether `weave rpc` gets a command for it, and whether the MCP server offers it as a tool. Most methods are on both. A few are on neither and belong to the panel alone, such as emptying the archive, and a few are a command but not a tool, such as connecting a forge with a token a model should never hold. A surface that is off carries the reason, and a method that says nothing is refused at build time.

The practical consequence of this, is that adding an endpoint gives you a CLI command and an MCP tool for free, every time, and taking one away is a deliberate line rather than an omission.

## The server architecture

Weave's architecture is built in a series of layers.

The outermost layer is the API handlers, the MCP server, and the CLI. These parse, validate, and translate. Interceptors sit in front of the handlers as cross-cutting rules: they log the call, attach who made it, and validate the request.

The next layer is the services, one per domain: agents, mail, marks, schedules, transcripts, pull requests, reviews, checkouts, scripts, worktrees, and forges. The business logic lives here. A handler does nothing but translate a request into a call on the service for its domain.

The stores hold the data, again one per domain, and own the database transactions. A store reads and writes the rows of its own resource and nothing else. It says nothing about publishing, because the database does that on its own, described below.

Separately, adapters stand in front of the things weave does not own: tmux, the agent CLIs, and the scripts it runs. Each one is a seam. The harness adapter, for instance, is where the difference between Claude and Codex is kept, so nothing above it knows which CLI an agent runs.

## Rows are resources

State for weave is kept in Postgres, and anything that must survive a restart is written there.

Everything the panel draws is a row of one table. An agent is its row in the agents table, with a few facts about it read from the tables that describe it and nothing else: its metadata, its latest unread failure, where its local review stands. A workflow is its row with its configuration. A mark, a piece of mail, a pull request and a schedule are each rows of their own table. These are the resources, and every read in the API answers one of them: a get answers one row, a list answers rows of one kind, and neither joins anything to them.

Some facts an agent's row carries are learned from outside the database. Whether its pane is alive is read from tmux, and whether its worktree has moved since its last review is read from git. The loops that learn them write them onto the row like any other column, so the row is the whole record and nothing about an agent lives only in a process's memory.

## How a change reaches the panel

The database publishes its own changes. A trigger on each resource table announces the table and the key of a row the moment the write that moved it commits, and a write that rolls back announces nothing. The tables that describe a resource without being one, an agent's metadata for instance, announce the resource they belong to. The trigger carries no opinion about whether the change is worth anyone's attention: every write announces. Which tables carry a trigger is declared once, in Go, beside the code that reads those rows back, and a generated migration puts them on.

The service holds one connection open to hear those announcements. It reads each named row back fresh and sends it down every open watch stream, or sends a tombstone when the row has left the set the stream carries, as an archived agent or a claimed mail has. A row that reads back exactly as the stream last carried it is sent to nobody, so a column a loop stamps on a timer moves nothing on the screen.

An announcement is an interrupt rather than a record. It carries no payload beyond the key, nothing durable is kept, and one that is lost costs a relist and never correctness, because every stream opens and reopens with the whole picture.

The panel holds one such stream open for its whole life. The stream opens with the whole picture, every row of every resource the panel draws, and then carries one row or one tombstone at a time. If the stream breaks for any reason, the service's hearing included, the panel opens it again and starts from the whole picture, so nothing that happened in the gap is missed. The panel never asks whether anything has changed; it is told.

In the usual vocabulary this is state with notification and reconciliation rather than an event system: the database holds what is true, an announcement only says to go and look and carries no authority of its own, nothing is kept as a replayable log of what happened, and a watcher that misses an announcement is put right by the next whole picture rather than left wrong.

## The combined row

The row a screen draws for an agent is more than the agent: it carries the mark on it, whether mail is waiting for it, and the pull requests opened from its branch with the current one's standing. Those are read off the agent, its workflow, the marks, the unclaimed mail and the pull requests, and joining them is defined once, as one function over the rows a caller holds.

The panel calls that function over the rows the stream has delivered, after every batch. The service calls the same function when a model or a script asks for agents over [MCP](mcp.md) or the CLI, so `list_agents` answers what the panel shows and there is no second way of putting the row together. A write's response carries only the row it wrote.

## The loops

The server runs a few loops that reconcile what the database says with what tmux says.

The reconcile loop runs every two seconds. This loop is responsible for reconciling the state of the agent with the view in the manager panel. For instance, the agent's "thinking" state is determined based on the presence of text in the render window that looks like thinking text, not based on stop, start or other hooks, which are unreliable.

The schedule reconciler loop runs every ten seconds. This makes the job queue's registrations of work match the schedules table.

The mail delivery loop runs every two seconds. This finds mail (agent-to-agent messages) that the recipient hasn't read yet, and makes sure it's delivered. This handles instances where an agent has stopped and would otherwise miss messages, and ensures that all agent-to-agent communications are eventually delivered. This makes multi-agent orchestration patterns possible.

Finally, the transcript sync runs every fifteen seconds. It folds each agent's new conversation text into the search index, so that you and other agents can search previous conversations in Postgres more easily.

## End to End

Let's walk through one behavior, end-to-end.

Imagine you press the archive key. The panel turns that keypress into an archive request and sends it to the service. Nothing about the panel's own state changes yet.

The request arrives at the service and passes the interceptors, which log it, attach caller info, and validate it against the constraints the API declares.

The handler then translates that request into a call on the agents service, which applies the rules: the agents must exist, and archiving them means stopping their panes as well as marking the rows.

The store then writes the rows, inside one transaction if more than one domain is involved. The service also kills the panes through the tmux adapter.

When the transaction commits, the database announces the rows it moved. The service reads them back, finds them gone from the live set, and sends the panel a tombstone for each, and the panel drops them from the picture it draws. If the call failed, the error is reported on the panel's screen and nothing was written.

The process is similar for an agent doing the same thing: the call may come over MCP, but eventually it still reaches the same validators, services logic, and database transaction code.
