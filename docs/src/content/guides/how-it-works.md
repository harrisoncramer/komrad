---
title: How it works
description: The service, the panel, tmux, and how a change reaches your screen.
group: start
order: 2
---

Komrade is built so that closing things is safe. You can close the panel, close your terminal, or restart your machine's session, and your agents keep running and nothing is forgotten. The rest of this guide explains how the pieces fit together to make that true.

## Two programs

Komrade is two programs, a service and a panel.

The service is the long-running part. It holds the database, answers the API and the MCP server, and watches every agent. It keeps running when you close the panel or your terminal.

The panel is what you look at. It is an ordinary client of the service. When it opens, it starts the service if nothing is running, and replaces it if a different build is running, so you are always talking to the binary you installed.

## Agents live in tmux

Each agent is a real terminal session running Claude or Codex. Komrade runs them inside a tmux server of its own, separate from the one you work in, so your own sessions and komrade's never tangle.

The panel lives there too, and reaches you through a tmux popup over whatever you were doing. Hiding the panel just closes the popup. Komrade's tmux server, the panel inside it, and every agent all keep running behind it.

<figure class="placeholder">
  <figcaption>A diagram of your tmux server with the komrade popup attached to komrade's own tmux server, which holds the panel and each agent's window.</figcaption>
</figure>

Because agents live in tmux rather than inside the service, restarting or upgrading the service leaves them untouched.

## One API for everything

Everything that changes anything goes through the service's API. The panel, a script using `komrade rpc`, and an agent calling MCP tools are all clients of that one API, so they follow the same rules.

The CLI commands and the MCP tools are generated from the API's own definitions. Every endpoint says which of the two it appears in, and says why when it is left out of one. So nothing can be added quietly to one surface and forgotten on the other. The [API reference](../../api/) is generated from the same definitions.

## Inside the service

The service is built in layers. The outer layer receives requests from the API, MCP and CLI, checks them, logs them and records who made them. Below that, one service per area, such as agents, mail or schedules, holds the rules. Below that, the stores read and write the database.

Anything komrade does not own, like tmux, the agent CLIs and your scripts, sits behind an adapter. The adapter for agent CLIs, for example, is the only place that knows how Claude and Codex differ.

## Everything is a row in Postgres

Komrade keeps everything that must survive a restart in Postgres. Each thing you see is one row: an agent, a workflow, a schedule, a piece of mail, a pull request, a review.

Some facts come from outside, such as whether an agent's terminal is alive or whether its branch has moved. Komrade's loops learn those and write them onto the row, so the database is always the whole story and nothing lives only in memory.

## How a change reaches your screen

The panel never asks whether anything has changed. The database tells it.

Whenever a write commits, the database announces which row changed. The service hears that, reads the row back and passes it to the panel, which redraws it. If the connection between them ever breaks, the panel reconnects and is sent the whole picture again, so it can never drift out of date. An announcement carries nothing but "this row changed", so losing one costs a refresh, never a wrong screen.

When a script or a model asks for agents, the service builds each row the same way the panel does. What an agent sees through MCP matches what you see.

## The background loops

A handful of loops keep the database in line with the outside world.

One watches every agent's terminal, reading the screen to tell whether the agent is working, waiting on you, or idle. It reads the screen rather than trusting hooks, because a hook can be missed and a screen cannot lie about what is on it.

Others keep schedules lined up with the job queue, nudge idle agents that have unread mail, fold new conversation text into the search index, follow each agent's working directory, measure worktrees, and check the forge for pull request changes.

## One action, end to end

Say you archive an agent from the panel. The panel sends an archive request to the service. The service checks it, stops the agent's terminal, and then marks the row archived in one transaction, together with any child agents that belong to it. The database announces the change, and the panel moves the agent into the archive.

If an agent asks to archive another agent over MCP, the request takes the same path with one extra check. Unless it is an orchestrator, an agent may only archive its own children.
