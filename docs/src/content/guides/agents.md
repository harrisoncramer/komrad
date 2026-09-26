---
title: Agents
description: What an agent is, what the panel shows about it, and how agents work together.
group: start
order: 3
---

This guide assumes you know what a [workflow](../workflows/) is, and you've created one.

Agents run your workloads. They are spawned according to workflows.

Right now, every agent is a Claude or Codex session. You can leave them working and come back, restart your machine without losing any history or context, search everything they have said, and even have agents spawn and manage other agents.

There are three types of agents, and you've already seen one: the orchestraotr agent(link to page). Orchestrator agents can spawn children called "implementer" agents, which each (typically) get their own worktree. Finally, implementer agents can spawn reviewer agents, which live inside _the same worktree_ and are responsible only for reviewing their work.

```
  workflow
   |
   |-- orchestrator      one per workflow, stands behind its row
   |                     hands work out, writes no code
   |
   |-- implementer       its own checkout, its own branch
   |     |
   |     '-- reviewer    a child, reads the parent's work
   |
   '-- implementer
         |
         '-- reviewer
```

Let's ask our orchestrator to spawn our first implementer agent. You can also hover over the orchestrator and press `c` to give the agent a name, and spawn it with a direct prompt, such as: "How many files are in this directory?"

<figure class="placeholder">
  example video
</figure>

## Agent State

Komrad reads each agent's screen to tell what it is doing. An agent is working while it thinks or runs a tool, waiting when it has stopped to ask you something, and idle when it has nothing to do.

Separately, an agent is either running or dormant. A dormant agent's CLI has exited, but its conversation is kept, and resuming picks up where it left off.

Each row shows the agent's state, whether mail (incoming messages from other agents) is waiting, and any pull request data. The details beneath show what it was launched with, its branch, how its checkout compares to the base, and its recent commits.

<figure class="placeholder">
  <figcaption>The details panel for one agent, showing its branch, its pull request and its recent commits.</figcaption>
</figure>

You can search the agent list by name, filter it, jump around it with marks(link to config on marks). Press KEYBINDING to cycle through the various views on the agent screen: the agent sidebar, the agent screen, and the inf

## Children

A child is any agent attached to a parent, and a reviewer is the usual case. Children share the parent's checkout, so [`usage.implementer.max_children`](../../config/#usage.implementer.max_children) caps how many one parent may hold. An agent that writes work of its own should be a standalone implementer, not a child.

<figure class="placeholder">
  <figcaption>A parent agent with a reviewer child indented beneath it in the agents list.</figcaption>
</figure>

## Picking up an existing branch

You can put an agent onto a branch someone else started, which komrad calls adopting it. The agent gets a fresh worktree on that branch, or works in the checkout that already has it. The branch stays its owner's. Komrad never resets it, and the agent is told the commits are not its to rewrite.

## Finishing with an agent

Archiving stops the agent and takes it out of the list, but keeps its record and conversation. Deleting is permanent: the record, the conversation and the worktree are all removed. Komrad refuses to run more agents than [`usage.max_agents`](../../config/#usage.max_agents) allows, so archive what you are done with.

Your agent is working in a fresh worktree, which means it has no dependencies installed. A [hook](../hooks/) fixes that.
