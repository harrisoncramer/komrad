---
title: How it works
description: The service, the panel, and how a change reaches your screen.
group: start
order: 2
---

## Two programs

Weave is two programs, a service and a panel.

The service is `weave serve`. It holds the database connection, answers the API and the MCP endpoint, and runs the loops that watch every agent. It keeps running when you close your terminal or the panel.

The panel is `weave panel`, the terminal interface you look at. It is an ordinary client of the service. If nothing is answering when it opens, it starts the service itself and then talks to it over HTTP.

## Tmux

A tmux server is a long-running process that owns sessions, windows and panes. You reach it through a socket.

Weave runs its own tmux server, separate from the one you work in. It is named by `WEAVE_TMUX_SOCKET`, which defaults to `weave`. It holds one session, called the manager session, and the panel runs inside it.

Pressing the weave key opens a tmux popup on your own server. That popup attaches to the manager session on weave's server. Hiding the panel detaches the popup, and the session and every agent keep running behind it.

On the agents list, `C-a` moves focus between the sidebar and the agent's pane. A pane running an agent passes every key straight to that agent, so this one key is bound outside tmux's prefix on weave's server. Weave only binds it while the agents list is showing, and the agent loses the key for that time. The key cannot be changed. If `C-a` is your own tmux prefix, your server takes it first and this binding never fires.

## One API for everything

Every action goes through the service's API, and the panel is just one client of it. Archiving an agent from the panel is a keypress that becomes an API call.

The same list of API methods produces the `weave rpc` commands and the MCP tools. Each method says whether it gets a command, a tool, or both. A method that is kept off a surface says why in its definition, and a method that says nothing fails the build. So adding an endpoint gives you a CLI command and an MCP tool every time, and leaving one out is always a decision someone wrote down.

The [API reference](../../api/) lists every method.

## Layers inside the service

The outermost layer is the API handlers, the MCP server and the CLI. They parse and check requests, then hand them on. In front of the handlers, interceptors log each call, record who made it, and validate it.

Behind the handlers are the services, one for each area such as agents, mail, schedules, reviews or worktrees. The rules live here.

Behind the services are the stores, again one for each area. A store reads and writes its own tables inside database transactions and touches nothing else.

Adapters sit in front of the things weave does not own, like tmux, the agent CLIs and your scripts. The harness adapter, for example, is the only place that knows the difference between Claude and Codex.

## Everything is a row

Weave keeps its state in Postgres. Anything that has to survive a restart is written there.

Each thing the panel draws is one row of one table. An agent, a workflow, a piece of mail, a pull request, a review and a schedule each have their own table. Every read in the API answers rows of one kind and never joins other things onto them.

Some facts come from outside the database. Whether an agent's pane is alive is read from tmux, and whether its worktree has moved is read from git. The loops that learn these facts write them onto the row, so nothing about an agent lives only in memory.

## How a change reaches the panel

The database announces its own changes. A trigger on each table sends the table and the row's key as soon as a write commits. A write that rolls back announces nothing.

The service listens for those announcements on one connection. For each one it reads the row back and sends it to every open watch stream. If the row has left what a stream carries, as a deleted agent has, it sends a tombstone instead. If the row reads back exactly as the stream last saw it, nothing is sent.

The panel keeps one watch stream open for as long as it runs. The stream starts with every row the panel draws, then sends one row or tombstone at a time. If the stream breaks for any reason, the panel opens it again and starts from the whole picture, so nothing in the gap is missed.

An announcement only says "go and look". It carries no data and nothing keeps it. Losing one costs a fresh read, never a wrong screen.

## The agent's row

The row drawn for an agent carries more than the agent. It shows the mark on it, whether mail is waiting, and the pull requests opened from its branch.

One function puts that row together from the rows a caller holds. The panel runs it over what the stream delivered. The service runs the same function when a script or a model asks for agents, so `list_agents` answers exactly what the panel shows.

## The loops

The service runs a few loops that keep the database in line with what tmux and the agents are doing.

The reconcile loop runs every two seconds. It decides each agent's state by reading its pane. An agent counts as thinking when its pane shows the CLI's working status line, rather than because a hook said so, since hooks are unreliable.

The schedule loop runs every ten seconds. It makes the job queue match the schedules table.

The mail loop runs every two seconds. It finds mail nobody has read yet and makes sure it gets delivered, including to agents that were stopped when it was sent. That is what makes multi-agent work reliable.

The transcript sync runs every fifteen seconds. It adds each agent's new conversation text to the search index, so you and other agents can search old conversations.

## One action, end to end

Say you press the archive key. The panel turns the keypress into an archive request and sends it to the service. Nothing on your screen changes yet.

The interceptors log the request, record who sent it and check it against the rules the API declares. The handler passes it to the agents service. That service checks the agents exist and knows that archiving means stopping their panes as well as marking their rows.

The store writes the rows in one transaction, and the service closes the panes through the tmux adapter. When the transaction commits, the database announces the rows. The service reads them back and sends them to the panel, which moves the agent into the archive. If the call failed, the panel shows the error and nothing was written.

An agent archiving another agent over MCP takes the same path through the same checks and the same transaction.
