# Workflows

Workflows are the blueprints used to create one first.

Every agent belongs to exactly one workflow, which is why weave makes you create one first.

## What a workflow holds

Since agents are just CLI sessions, workflows are buckets of defualt settings for those agents. For instance, a workflow might indicate which model to use (like opus or haiku), the working directory, or whether each agent gets a git worktree of its own, relevant for worktree developmemt.

Ultimately, each workflow setting for an agent can be overridden at the moment when an agent is spawned; this approach provides flexibility, for instance when you'd like to use a particular workflow, but with a different model or different directory.

Workflows also hold shared context. Represented as free text, this is appended to the system prompt of every agent created inside the workflow.

## Where the worktrees go

A worktree workflow makes its worktrees in the directory your [config](configuration.md) names under `[worktrees]`, which every workflow shares. Some repositories will not have that: their own tooling expects worktrees in a particular place, and finds nothing when an agent's checkout is somewhere else. A monorepo might assign each worktree under `.worktrees` its own ports and docker project, keyed on the directory it sits in.

A workflow can name its own parent directory for that reason. Set it and this workflow's worktrees are made there instead, still one per agent and still named after the agent. Left blank it falls back to the configured directory.

It reaches only agents spawned afterwards, since an agent already provisioned holds the directory it was given, and moving that out from under it would leave weave looking for a checkout that is no longer there.

Which CLI the agents run is known as the "harness." Currently, `claude` and `codex` are supported. Like all settings, agents already spawned keep the harness they started on, so changing this only affects new ones. A workflow's flags go with its harness: an agent that runs another CLI is launched without them, since `--model opus` means nothing to a CLI that has never heard of it.

## Excluding paths from the diff

The panel draws a diff of what each agent has written, and generated output drowns it: a regenerated protobuf or sqlc directory is thousands of lines nobody reads. A workflow can name paths to leave out, as directories or globs relative to the agent's checkout, such as `proto/gen` or `*.pb.go`.

The list is read every time a diff is drawn rather than snapshotted at launch, so adding to it fixes the agents already running as well as the ones spawned afterwards.

## The context

Weave assembles the appended system prompt in three layers, general to specific: the standing briefing about weave itself (the MCP, how to send mail, etc), your global context file, shared across all agents, and then the workflow's context. The workflow comes last, so it reads as the final word on anything the layers above it also mention. The global file is configured under `[context]` in your [config](configuration.md).

This context could include general conventions, such as:

- Repo conventions. Where things live, which generator owns which directory, how tests are run.
- Naming rules. Branch names, commit style, what an agent should call the PR it opens.
- Boundaries. What agents in this group are allowed to touch, and what they must leave alone.
- Where to report. Which metadata keys to record on themselves so you can find their work later.

The context is baked into the agent's system prompt at launch, so editing a workflow reaches only the agents spawned after the edit. Agents already running keep what they started with.

## Archiving and restoring

Archiving a workflow hides it and archives every agent inside it, in one transaction. The agents go with it because there is nowhere else for an agent to live. Nothing is destroyed: the rows stay, the transcripts stay, and `search_transcripts` still finds the conversations.

It is refused while any schedule still launches into the workflow, since a schedule reads its directory and flags off the workflow when it fires and would have nowhere to run. Turn those schedules off first, deliberately.

Archiving does not remove an agent's worktree, so whatever was provisioned is still on disk.

Restoration from the archive is all or nothing. It is refused if the returning agents would not fit under your configured agent ceiling.

## Setting up your first one

Open the panel with `prefix + g`. Press `w`, give it a name, leave everything else blank, and submit. Press `c` and give an agent a prompt. That is the whole minimum: a workflow with nothing but a name is just a folder.

Then add to it as you learn what the group needs. A default directory once you are tired of typing it. Context once you have told three agents the same thing. A worktree per agent once you want them out of each other's way. A pre-launch hook once that worktree needs something installed in it.
