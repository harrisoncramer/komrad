---
title: How it works
description: The service, the panel, tmux, and the one API everything goes through.
group: architecture
order: 11
---

You have used every piece: [workflows](../workflows/), [agents](../agents/), [hooks](../hooks/), [schedules](../schedules/), [mail](../mail/) and [MCP](../mcp/). This guide steps back and shows how they fit together, and why closing things is always safe. The guides after it go deeper.

## Two programs

```
  your tmux                komrad's tmux
  +-------------+          +-------------------------------+
  |  popup  ----+--------->|  panel   agent   agent   ...  |
  +-------------+          +-----+-------+-------+---------+
                                 |       |       |
                                 v       v       v
                           +-------------------------------+
                           |  komrad serve                |
                           +---------------+---------------+
                                           v
                                       Postgres
```

Komrad is a service and a panel. The service runs in the background. It owns the database, answers the API, and watches every agent. The panel is just a screen. It talks to the service and remembers nothing itself. You can close it whenever you like.

## Agents live in tmux

Each agent is a real terminal running Claude or Codex. Komrad runs them in a tmux server of its own, so they never get mixed up with your own windows. The panel lives there too. When you open it, your tmux shows a popup that attaches to komrad's session. Hiding the popup hides the panel and nothing else.

This is also why you can upgrade komrad without losing your agents. The service restarts. The tmux server does not.

## One API

Everything goes through one API. The panel uses it. A script using `komrad rpc` uses it. An agent calling an MCP tool uses it. So they all get the same rules and the same checks.

The CLI commands and MCP tools are not written by hand. Every endpoint is declared once, and the declaration says which surfaces it appears on. The commands, the tools, and the [API reference](../../api/) are all built from that.

## Everything is a row

Everything komrad knows is a row in Postgres. An agent, a workflow, a piece of mail, a review. Facts from outside, such as whether an agent is idle, are found by background loops and written onto the row. Nothing important lives only in memory.

## How a change reaches your screen

The panel never asks whether something changed. When a row is written, Postgres says so. The service reads the row back and sends it to the panel over a stream. If the stream breaks, the panel reconnects and gets a fresh copy of everything.

For instance, say you archive an agent. The panel sends a request. The service checks it, kills the agent's terminal, and marks the row archived. Postgres announces the change, and the panel redraws. If an agent archives another agent over MCP, it goes through exactly the same path.

That is the mechanism. [Philosophy](../philosophy/) is the reasoning behind it.
