---
title: Workflows
description: The templates agents are created from, and what each one decides for them.
group: concepts
order: 3
---

A workflow is the template an agent is created from. Every agent belongs to exactly one workflow, which is why weave asks you to make one before your first agent.

Think of a workflow as a group of agents that do the same kind of work. The workflow decides where they start, how their CLI is launched, what they are told before they begin, and whether they each get a checkout of their own.

<figure class="placeholder">
  <figcaption>The agents list, with two workflows expanded and their agents grouped beneath each one.</figcaption>
</figure>

## What a workflow decides

A workflow names the directory its agents start in, which agent CLI they run, and the flags that CLI is launched with, such as which model to use. When you spawn an agent you can change any of these for that one agent, so a workflow is a starting point rather than a cage.

Changing a workflow only affects agents spawned afterwards. An agent keeps the setup it was launched with.

## Shared context

A workflow can hold free text that is added to the system prompt of every agent in it. This is where you put what you would otherwise tell each agent by hand: where things live in the repository and how tests run, how branches and commits are named, what agents in this group may touch and what they must leave alone, and how they should record their work so you can find it later.

The context an agent starts with is built up in layers, from general to specific. First comes weave's own briefing about itself, then a note about the agent's role if it has one, then your [global context file](../../config/#context), and last the workflow's own text. The workflow comes last, so it has the final word.

## A checkout per agent

Agents that write code need to stay out of each other's way. A workflow can give every agent its own git worktree, a separate checkout of the repository, cut fresh from a base branch. The agent works on a branch of its own, and its worktree is removed when the agent is deleted.

For this to work, the workflow's directory has to be an ordinary clone of the repository.

Worktrees are made in the directory set by [`worktrees.dir`](../../config/#worktrees.dir), which every workflow shares. A workflow can name its own directory instead, for repositories whose own tooling expects checkouts in a particular place.

New worktrees start as clean checkouts, so anything git does not track, like a local environment file, is missing. A workflow can list files to copy or link into every new worktree. For anything heavier, like installing dependencies, use a [hook](../hooks/).

## The base branch

A worktree workflow names a base branch. New worktrees are cut from it, an agent's work is measured and reviewed against it, and rebases move an agent onto it. Weave works it out from your clone when you create the workflow and remembers it.

If the clone ever loses that branch, weave says so rather than guessing, and `weave doctor` lists every workflow whose base branch cannot be found.

## Leaving generated code out of the diff

In a worktree workflow, the panel shows a diff of everything an agent has written. Generated code can bury the parts worth reading, so a workflow can list paths to leave out of the diff. This takes effect straight away, for agents already running as well as new ones.

<figure class="placeholder">
  <figcaption>An agent's diff in the panel, with a generated directory left out.</figcaption>
</figure>

## Archiving and restoring

Archiving a workflow puts it and every agent in it away together. Nothing is destroyed. The records, the conversations and the worktrees are all kept, and search still finds what the agents said.

A workflow that a schedule still launches into cannot be archived, because the schedule would have nowhere to run. Archive the schedule first.

Restoring brings the whole workflow back with its agents, stopped, ready to resume. It is refused if the returning agents would go over your agent limits, or if one would clash with the name of an agent that is already live.

## Starting small

A workflow with nothing but a name is perfectly useful as a folder. Start there and add to it as you notice a pattern. Set a default directory once you are tired of typing it. Add context once you have told three agents the same thing. Turn on worktrees once agents start treading on each other. Add a hook once those worktrees need setting up.
