---
title: Workflows
description: The templates agents are created from, and what each one decides for them.
group: start
order: 2
---

You have [installed komrad](../install/) and the panel is open and empty. Before you can start an agent, you need a workflow, because every agent belongs to one. A workflow is the template an agent is created from. Think of it as a group of agents that do the same kind of work.

## Creating one

Press `w` in the panel. Give the workflow a name and a directory, say `myapp` and `~/code/myapp`, and turn worktrees on if that directory is a git clone. Komrad works out the base branch from the clone. That is enough to start with. Everything below can be added later with `e` on the workflow's row, or from a script with `komrad rpc create-workflow`.

<figure class="placeholder">
  <figcaption>The agents list, with two workflows expanded and their agents grouped beneath each one.</figcaption>
</figure>

## What a workflow decides

A workflow says where its agents start, which CLI they run, and what flags it is launched with, such as the model. When you spawn an agent you can override any of these for that one agent. Changing a workflow only affects agents spawned afterwards.

## Shared context

A workflow can hold text that is added to every agent's system prompt. This is where you put what you would otherwise tell each agent by hand: how tests run, how branches are named, what to leave alone. It comes after komrad's own briefing and your [global context](../../config/#context), so it has the final word.

## A checkout per agent

Agents that write code need to stay out of each other's way. A workflow can give each agent its own git worktree, cut fresh from a base branch. The agent works on a branch of its own, and the worktree is removed when the agent is deleted. The workflow's directory has to be an ordinary clone for this to work.

Worktrees go in [`worktrees.dir`](../../config/#worktrees.dir) unless the workflow names its own place. A new worktree is a clean checkout, so a workflow can list files to copy in, like a local environment file. For anything heavier, like installing dependencies, use a [hook](../hooks/).

The base branch is worked out from your clone when you create the workflow. New worktrees are cut from it and an agent's work is measured against it. If the clone loses that branch, `komrad doctor` says so.

A workflow can also list paths to leave out of the diff the panel shows, so generated code does not bury the parts worth reading.

## Archiving and restoring

Archiving a workflow puts it and every agent in it away together. Nothing is destroyed, and search still finds what the agents said. Restoring brings the whole workflow back with its agents stopped and ready to resume. A workflow a schedule still launches into cannot be archived until the schedule is.

## Starting small

A workflow with nothing but a name is a perfectly good folder. Add a directory once you are tired of typing it. Add context once you have told three agents the same thing. Turn on worktrees once agents start treading on each other.

You have a workflow. Now spawn an [agent](../agents/) into it.
