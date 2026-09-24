---
title: Agents
description: Creating, finding, searching and cleaning up the agents weave runs.
group: concepts
order: 4
---

An agent is a Claude or Codex session that weave runs in a tmux pane. You can step away from it, restart your machine, search what it said, or have other agents drive it.

Some common setups show what that makes possible. A worktree agent gets an isolated checkout, and the worktree goes when you delete the agent. A triage agent runs on a [schedule](../schedules/), for example every weekday morning with a prompt to read the overnight alerts and write up anything that needs a person. An orchestrator is an agent whose job is to hand out work. It uses weave's [MCP](../mcp/) tools to create other agents and talks to them by [mail](../mail/).

## Creating one

Press `c` in the sidebar. If you name no harness, weave uses `default_harness` from your config. You can also create agents from the CLI.

## States

An agent is thinking when it is generating or running a tool. It is waiting when it is blocked on a question for you, and idle when it has nothing to do.

An agent shown as a grey `?` is uncertain. Weave captured its pane but could not read it. That is not the same as finished, because the unreadable screen might be a question waiting for you. So weave leaves uncertain agents out of auto-archiving, holds schedules back for them, and holds their mail until the pane reads again.

Separately, an agent is either running or dormant. A dormant agent's CLI has exited and its pane is closed. You can bring it back by resuming its session.

## Adopting a branch that already exists

Adopting puts an agent on a branch someone else made. Hover a worktree workflow's header, press `a`, and give it a branch name. It works the same on GitHub, GitLab, a self-hosted remote, or a repository with no forge at all, because a branch belongs to git. A pull request URL or number is refused, and the error says what to use instead.

If nothing has the branch checked out, the agent gets a new worktree on it, fetched from origin first. If another checkout already holds the branch, the agent is launched in that checkout as it stands. If the branch is checked out in the workflow's own directory, adoption refuses, because that is the checkout you work in.

The branch stays its owner's. Weave never resets it, and deleting the agent removes the worktree but leaves the branch. Adopting a branch that already has an agent is refused and names that agent.

The agent's prompt starts by telling it the branch and that the commits are not its own to rewrite. If you leave the prompt blank, it is told nothing has been asked yet and to wait. Agents can adopt branches too, through `adopt` over MCP.

## Children

A child agent is attached to another agent. It runs in the parent's directory, sets up nothing of its own, can only mail its own family, and goes when the parent goes. The panel draws it indented under its parent.

Use a child when the new agent will read the parent's work rather than write its own. A reviewer or a test runner is the usual case. An agent that writes its own work needs its own checkout, so make it a standalone agent instead.

Press `C` on an agent to attach a child. The form asks for a prompt, a title, the harness and flags, but not where to run, because the parent decides that. Agents attach children through `create_agent` over MCP, with `parentAgentId` set to their own id.

A child is a guest in someone else's checkout. It is told to read and run things but not to commit, push, stash or switch branches. It cannot have children of its own, and it can only mail its parent and that parent's other children.

Archiving or deleting a parent takes its children with it. You can also archive a child by itself, which is how you finish with a reviewer while its parent keeps working. A child can only be restored once its parent is live again.

All of a parent's children share one checkout, so `max_children` in your config caps how many one agent may hold. A request over the cap is refused, not queued.

## Metadata

Metadata is how an agent records things you will want to open later, like the ticket it is working on. It is a set of key and value pairs on the agent, written by the agent with `set_agent_metadata` using its own id from `WEAVE_AGENT_ID`.

The panel shows metadata in the details under the list. The key `pr` is refused, because weave reads pull requests from the forge itself.

## Pull requests

The third symbol on an agent's row is the pull request opened from its branch, read by the [pull request sync](../forges/#what-weave-reads). If the branch has several, the row shows the newest one that is still open, or the newest one if all are finished.

A grey pull request symbol means it is open. A green check means the forge would merge it now. Yellow means changes were requested, and a grey draft symbol means it is a draft. A red cross means a check failed, and a red stop sign means it conflicts with its base. Purple means merged, and red means closed without merging. Nothing is drawn until the repository has been read, or if no pull request was opened from the branch.

The details show a `pull request` row for the current one, such as "#740 merged 2h ago", and a `pull requests` row listing the others. Over MCP, `list_agents` and `get_agent` include each agent's pull requests.

## The details pane

The details under the list describe the row the cursor is on. For an agent that is what it was launched with, its branch and pull requests, how its worktree stands against the base, its metadata, and up to ten uncommitted paths and ten commits. The worktree facts are read when the cursor rests on the row and again every minute, so they can be a minute old.

`ctrl+t` moves the keys into the details, which then grow to fill the sidebar. `/` searches the details, `tab` turns the search into a filter, and `n` and `N` step between matches. `esc` clears the search, and a second `esc` hands the keys back.

## The git menu

`b` opens the git actions for the agent under the cursor. `r` rebases its worktree onto the latest base, and `o` opens its current pull request in your browser. An action that cannot apply says why instead of doing nothing, for example when the workflow has no worktrees or the branch has no pull request.

## Marks

A mark is a letter you put on a row so you can jump back to it, the way marks work in vim. Press `m` and a letter to mark the row under the cursor. Press `'` and that letter from anywhere in the list to jump back. Marked letters show in the column down the left edge.

Agents and workflows share the same letters, and each letter belongs to one row. Marking a row with a letter that is already used moves the mark. Pressing `m` and a row's own letter clears it.

Pressing `'` twice jumps back to where you were before the last jump. A jump always lands, even if it has to reveal a hidden row. Marks are only yours, so no agent can set one, and a row loses its mark when it is archived.

## Searching the list

Press `/` and type. Matches light up in both the list and the details, and nothing moves while you type.

`enter` applies the search to the pane that has the keys. In the list, the cursor moves to the nearest agent whose name matches. In the details, it moves to the nearest matching field. `n` and `N` then step through matches.

`ctrl+t` moves the keys between the list and the details and keeps the search. So `/directory`, then `ctrl+t`, then `y` copies the hovered agent's directory.

`tab` while typing turns the search into a filter, which hides what does not match in that pane only. `esc` gives up the search and puts the cursor back where it was. There is only one search at a time.

## Archiving, restoring and deleting

Archiving an agent, with `z` by default, stops it, closes its pane and takes it off the panel. The row, its metadata and its transcript are kept. Archiving is the normal way to finish with an agent.

The archive prompt also offers "Archive and delete", which archives and then destroys the agent.

`Z` opens the archive, where you can restore agents or delete them for good. Deleting removes the row, its metadata, its transcript and its search index, and it removes the agent's worktree from disk. Search will never find that conversation again. `v` and `V` select several agents at once.

An archived workflow can be deleted from the archive with `d`. Its archived agents go through the same cleanup as deleting them directly. If any of that cleanup is still running or fails, the workflow stays archived until nothing is left in it.

`D` clears the whole archive. It asks first and then runs in the background, because each agent's worktree has to be removed. Rows still being cleaned up stay until they finish, so press it again if anything remains. A row whose cleanup failed stays with a red `✗`, and the stage beside it says what went wrong. Empty archived workflows are deleted along the way.
