---
title: Agents
description: What an agent is, what the panel shows about it, and how agents work together.
group: start
order: 3
---

With a [workflow](../workflows/) in place, move the cursor onto it and press `c`. Give the agent a name and a prompt, say `fix-login` and a description of the bug, and it starts. That is an agent: a Claude or Codex session komrade runs for you in a terminal of its own. You can leave it working and come back, restart your machine without losing it, search everything it has said, or have other agents give it work.

<figure class="placeholder">
  <figcaption>The agents list beside an agent's live terminal, with the details panel for that agent underneath.</figcaption>
</figure>

## Three kinds of agent

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

Every agent has a role, and the role decides what it can do.

An implementer is the ordinary agent. It gets a checkout of its own, works on its own branch, and lands the change. Most agents you spawn are implementers.

A reviewer is a child of an implementer. It runs inside its parent's checkout, is told not to change it, and reads the parent's work to write a review. It can only mail its own family, and it goes away when its parent does.

An orchestrator is one agent per workflow. It writes no code and has no checkout. It spawns implementers into its own workflow through [MCP](../mcp/), talks to them by [mail](../mail/), and gathers what they find. Landing on a workflow's row in the panel puts you in a conversation with it, and that conversation accumulates for the life of the workflow. It cannot spawn into another workflow, and it cannot have children of its own.

A scheduled agent is not a fourth kind. It is an implementer that a [schedule](../schedules/) started on a timer.

## What state an agent is in

Komrade reads each agent's screen to tell what it is doing. An agent is working while it thinks or runs a tool, waiting when it has stopped to ask you something, and idle when it has nothing to do. If komrade cannot read a screen, it treats the agent as waiting on you, since an unreadable screen might be a question.

Separately, an agent is either running or dormant. A dormant agent's CLI has exited, but its conversation is kept, and resuming picks up where it left off.

## What the panel shows

Each row shows the agent's state, whether mail is waiting, and its pull request if it has one. The details beneath show what it was launched with, its branch, how its checkout compares to the base, its recent commits, and any metadata it has recorded about its own work, such as the ticket it is on.

<figure class="placeholder">
  <figcaption>The details panel for one agent, showing its branch, its pull request, its recorded metadata and its recent commits.</figcaption>
</figure>

You can search the list by name, and search every conversation komrade has ever run, archived ones included. Marks let you jump straight back to an agent you care about, the way marks work in vim.

## Children

A child is any agent attached to a parent, and a reviewer is the usual case. Children share the parent's checkout, so [`usage.implementer.max_children`](../../config/#usage.implementer.max_children) caps how many one parent may hold. An agent that writes work of its own should be a standalone implementer, not a child.

<figure class="placeholder">
  <figcaption>A parent agent with a reviewer child indented beneath it in the agents list.</figcaption>
</figure>

## Picking up an existing branch

You can put an agent onto a branch someone else started, which komrade calls adopting it. The agent gets a fresh worktree on that branch, or works in the checkout that already has it. The branch stays its owner's. Komrade never resets it, and the agent is told the commits are not its to rewrite.

## Finishing with an agent

Archiving stops the agent and takes it out of the list, but keeps its record and conversation. Deleting is permanent: the record, the conversation and the worktree are all removed. Komrade refuses to run more agents than [`usage.max_agents`](../../config/#usage.max_agents) allows, so archive what you are done with.

Your agent is working in a fresh worktree, which means it has no dependencies installed. A [hook](../hooks/) fixes that.
