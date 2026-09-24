---
title: Workflows
description: The templates agents are created from, and what each one holds.
group: concepts
order: 3
---

A workflow is the template an agent is created from. Every agent belongs to exactly one workflow, which is why weave asks you to make one first.

## What a workflow holds

An agent is a CLI session, so a workflow is a set of defaults for those sessions. It can name the directory agents start in, the flags they run with (such as which model), and whether each agent gets a git worktree of its own.

You can override any of these when you spawn an agent. That lets you use a workflow's setup but change the model or the directory for one agent.

A workflow also holds shared context. This is free text added to the system prompt of every agent created in it.

## The harness

The harness is the agent CLI the workflow's agents run. Weave supports `claude` and `codex`.

An agent keeps the harness it started with, so changing this only affects new agents. A workflow's flags belong to its harness. An agent that runs a different CLI is launched without them, because flags like `--model opus` mean nothing to a CLI that does not know them.

## The base branch

Every workflow names a base branch. Worktrees are cut from it, review rounds are diffed against it, and rebases move agents onto it.

If you leave it blank, weave fills it in from the clone at the workflow's directory. It takes what `origin/HEAD` names, then the branch the checkout is on, then `main`. After that it is stored, and editing the workflow later requires one.

If the clone does not have the base branch, weave says so rather than guessing. A rebase or review round refuses and names the branch, and `weave doctor` lists every workflow with a base its clone cannot find. Fetch the branch, or change the workflow to one the clone has.

## Where worktrees go

Worktree workflows make their worktrees in the directory named under `[worktrees]` in your [config](../../config/). Every workflow shares that directory.

Some repositories need their worktrees somewhere specific. A monorepo might give each worktree under `.worktrees` its own ports and docker project, based on where it sits. So a workflow can name its own parent directory, and its worktrees are made there instead. Each agent still gets one worktree, named after the agent.

This only affects agents spawned afterwards. An agent that already has a directory keeps it.

## Leaving paths out of the diff

The panel shows a diff of what each agent has written. Generated code can bury it, because a regenerated protobuf or sqlc directory is thousands of lines nobody reads.

A workflow can list paths to leave out, as directories or globs relative to the agent's checkout, such as `proto/gen` or `*.pb.go`. The list is read every time a diff is drawn, so adding to it fixes agents that are already running.

## The context

Weave builds the added system prompt in three layers, from general to specific. First comes weave's own briefing, which covers the MCP tools, mail and so on. Next is your global context file, set under `[context]` in your [config](../../config/). Last is the workflow's context, so it has the final word.

Good workflow context covers the repo's conventions, like where things live and how tests run. It covers naming, like branch names and commit style. It covers what agents in the group may touch and what they must leave alone, and which metadata keys they should record so you can find their work.

The context is set when an agent launches. Editing a workflow reaches only agents spawned after the edit.

## Archiving and restoring

Archiving a workflow hides it and archives every agent in it, in one transaction. Nothing is destroyed. The rows and transcripts stay, and search still finds the conversations. Worktrees stay on disk too.

Weave refuses to archive a workflow while a schedule still launches into it, because the schedule would have nowhere to run. Turn those schedules off first.

Restoring brings everything back or nothing. It is refused if the returning agents would push you over your agent limit.

## Your first workflow

Open the panel with `prefix + g`. Press `w`, give the workflow a name, leave everything else blank and submit. Then press `c` and give an agent a prompt. A workflow with only a name works fine as a folder.

Add to it as you learn what the group needs. Set a default directory once you are tired of typing it. Add context once you have told three agents the same thing. Turn on worktrees once agents need to stay out of each other's way, and add a pre-launch [hook](../hooks/) once those worktrees need something installed.
