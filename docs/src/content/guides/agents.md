---
title: Agents
description: What an agent is, what the panel shows about it, and how agents work together.
group: concepts
order: 4
---

An agent is a Claude or Codex session that weave runs for you in a terminal of its own. You can leave it working and come back, restart your machine without losing it, search everything it has said, or have other agents give it work.

<figure class="placeholder">
  <figcaption>The agents list beside an agent's live terminal, with the details panel for that agent underneath.</figcaption>
</figure>

## What agents are good for

The same few ideas cover very different jobs. A worktree agent gets a private checkout of your repository and works on its own branch, so several can change code at once without colliding. A scheduled agent wakes on a timer, say every weekday morning, to read the overnight alerts and write up what needs a person. An orchestrator hands work out: it starts other agents through weave's [MCP](../mcp/) tools, talks to them by [mail](../mail/), and gathers what they find.

## What state an agent is in

Weave reads each agent's screen to tell what it is doing. An agent is working while it thinks or runs a tool, waiting when it has stopped to ask you something, and idle when it has nothing to do. The panel shows this at a glance, so you can see which agents need you.

Sometimes weave cannot make sense of a screen. It treats that agent as though it were waiting on you, because an unreadable screen might be a question. It holds back scheduled runs and mail for that agent until the screen reads normally again.

An agent whose setup failed stays in the list and says what went wrong, so you can fix the cause and try again.

Separately from what it is doing, an agent is either running or dormant. A dormant agent's CLI has exited and its terminal is closed, but its conversation is kept, and resuming it picks up where it left off.

## What the panel shows about an agent

Each agent's row shows its state, whether mail is waiting for it, and the pull request its branch has open, if any. The pull request symbol changes as the pull request moves: open, approved, failing checks, conflicting, merged, or closed.

Beneath the list, the details show everything about the agent under the cursor: what it was launched with, its branch and pull requests, how its checkout compares to the base branch, the facts it has recorded about its own work, and its latest changes and commits.

<figure class="placeholder">
  <figcaption>The details panel for one agent, showing its branch, its pull request, its recorded metadata and its recent commits.</figcaption>
</figure>

Agents record facts about their own work as metadata, such as the ticket they are on or a design note they wrote. That is how you find your way back to what an agent did, long after its conversation has scrolled away.

## Finding agents and what they said

You can search the list by name, and search the details of the agent you are looking at. You can also search every conversation weave has ever run, archived ones included, to find the agent that discussed something weeks ago.

Marks let you jump straight back to an agent or a workflow you care about, the way marks work in vim. They belong to you, and no agent can set one.

## Agents that work together

A child agent is attached to a parent agent and reads the parent's work. A reviewer checking a change, or a test runner reporting failures, is the usual case. A child works inside its parent's checkout, is told not to change it, can only mail its own family, and goes away when its parent does. The panel draws it beneath its parent. How many children one agent may hold is capped by [`max_children`](../../config/#max_children), because they all share one checkout.

An agent that writes work of its own needs its own checkout, so it should be a standalone agent rather than a child.

<figure class="placeholder">
  <figcaption>A parent agent with a reviewer child indented beneath it in the agents list.</figcaption>
</figure>

## Picking up an existing branch

You can put an agent onto a branch that already exists, one that someone else started, which weave calls adopting it. This works on any git repository, whether or not it lives on a forge. If nothing has that branch checked out, the agent gets a fresh worktree on it. If another checkout already has it, the agent works in that checkout as it is.

The branch stays its owner's. Weave never resets it, deleting the agent leaves the branch alone, and the agent is told at the start that the commits are not its to rewrite.

## Finishing with an agent

Archiving is the ordinary way to finish. It stops the agent, closes its terminal and takes it out of the list, but keeps its record and its conversation, so search still finds it and you can restore it later.

Deleting is permanent. It removes the record, the conversation and its search index, and it removes the agent's worktree from disk. You can delete agents from the archive one at a time, several at once, or clear the whole archive. Weave does the cleanup in the background and shows any agent whose cleanup failed, with the reason.

Weave refuses to run more agents than [`max_agents`](../../config/#max_agents) allows, so archiving what you are done with keeps room for new work.
