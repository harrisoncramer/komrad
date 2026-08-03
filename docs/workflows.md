# Workflows

A workflow is a named bucket of related [agents](agents.md), and the template they are created from. It runs nothing on its own. Every agent belongs to exactly one workflow.

A workflow holds:

- A name. Agent names are unique within a workflow, so two workflows can each have an agent called `auth`.
- Context. Free text appended to the system prompt of every agent created inside it.
- A default directory and flags, prefilled into the new-agent form and used by any [schedule](schedules.md) firing into the workflow. An agent that names its own wins.
- A harness, `claude` or `codex`. Blank takes your `default_harness`. Agents keep whatever they started on.
- A setup action and a cleanup action, both naming a file in your actions directory. See [actions](actions.md).
- Other settings and configuration for new agents spawned under it.

## Context

Put the ground rules here and keep the task in the prompt. Repo conventions, branch and commit style, what agents in this group may not touch, which metadata keys to record on themselves.

Weave stacks three layers, general to specific: its own briefing about the panel, then your global context file from `[context]` in the [config](configuration.md), then the workflow's. The workflow comes last so it wins. Context is baked in at launch, if you need to send an agent a new udate

## Setup and cleanup

Setup runs before the tmux window is opened, and again before a dormant agent is resumed. It blocks, with a ten minute ceiling. Exit non-zero and the launch is abandoned: no window, no row, no agent.

Whatever it prints on stdout is the directory the agent runs in. Print nothing and the agent runs where the workflow or the form said. Progress messages go on stderr, since stdout is a directory and a chatty script gets refused. The script gets `WEAVE_TRIGGER=setup` on a create and `WEAVE_TRIGGER=resume` on a resume, so make it safe to run twice.

Cleanup fires when an agent is deleted for good, never when it is archived. Nothing waits on it, and a failure is logged rather than blocking the delete.

## A worktree per agent

The common shape, and the reason the two fields exist. Setup provisions a git worktree and prints its path, so every agent in the workflow works in a checkout of its own and none of them collide. Cleanup removes it when the agent is deleted. [Actions](actions.md) has both scripts to copy.

Once they are in your actions directory, press `w` and name them as the workflow's setup and cleanup actions. Nothing else changes: you start agents the same way, and each one lands in its own tree.

## From the panel

The `w` key creates a workflow from anywhere in the agents list. On a workflow header, `e` edits, `r` renames, `z` archives. The two action fields are pickers listing what your actions directory actually holds, and `ctrl+e` opens `$EDITOR` for the context field.

## From MCP

```
create_workflow {
  "name": "myrepo features",
  "context": "Work only inside the worktree you were started in.",
  "cwd": "~/myrepo",
  "flags": "--model opus",
  "harness": "claude",
  "setupAction": "create-worktree",
  "cleanupAction": "cleanup-worktree"
}
```

`list_workflows` pages through them and takes an `archived` flag. `update_workflow` replaces every field, so read the workflow first and send the whole thing back.

## Archiving

Archiving a workflow archives every agent inside it, since there is nowhere else for an agent to live. Nothing is destroyed and transcripts stay searchable. It is refused while a live schedule still launches into the workflow, so turn those off first.
