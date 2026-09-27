---
title: Philosophy
description: An overview of the design decisions behind komrad, the terminal-based agent orchestrator.
group: architecture
order: 12
---

Welcome, komrad!

Komrad makes a few choices that look unusual, on the surface, for agent orchestration tooling. This guide explains why.

## Postgres... really?

Everything komrad knows is written to a Postgres database before it is acted on. Close the panel, close the terminal, log out, and nothing is lost. An archived agent can be restored and its conversation searched. [How it works](../how-it-works/) covers the mechanics.

Most agent orchestration tools ship a simpler persistence layer. Komrad does not.

Postgres is heavier than a file in your home directory, and you have to run it. Komrad uses it anyway, because a file cannot deliver the things that make orchestration worth having: change notifications, several clients at once, real queries, and full-text search. If running Postgres is a dealbreaker, that is a fair take. Komrad might not be for you.

## komrad is terminal-based

Komrad is designed for people who already work in a terminal. It is not a desktop app.

Each agent is a real terminal in a real tmux server. Taking over from an agent means attaching to its pane. Any CLI that runs in a terminal works. The panel is a tmux session you attach to, driven from the keyboard, and every key is configurable.

## komrad is flexible

komrad gives you workflows, agents, hooks, schedules and mail. It does not say how to combine them. The same pieces run a review loop, an overnight batch of fixes, or an agent that reads an inbox each morning.

Nothing is set up for you. Other orchestration tools push you down one path. They are faster to start with, as long as you share their opinion, and they break down when your work does not fit.

That does not mean there is no easy path. Worktrees work out of the box, for instance. But you do not have to use them, and hooks let you run your own setup and teardown to shape a workflow however you like.

## komrad uses one unified API

Komrad ships one API. The panel is one client of it, and so is an agent calling an MCP tool. Every client gets the same validation and the same permission checks. An agent can also query komrad's database with a read-only login, so it can answer questions nobody wrote a tool for.

The CLI, the MCP tools and the docs are all generated from that same API.

## komrad runs on komrad

Komrad's own GitHub repository is worked on by komrad agents, running on a remote box through a dedicated GitHub App, finding and fixing issues on their own.

That said, there is no komrad account and no hosted service.

Komrad runs on your machine, or on one you reach over SSH. Your forge token sits in a file only your user can read, and agents are handed short-lived tokens instead of the real one. Locally, agents can have the same MCP tools and permissions you do. Remotely, you set the environment up however you want.

Komrad is MIT licensed, so fork it or change it freely.

The rest of the architecture guides start with [Events and the watch](../architecture-events/), the mechanism behind the Postgres choice.
