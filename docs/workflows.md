# Workflows

Workflows are the blueprints used to create one first.

Every agent belongs to exactly one workflow, which is why weave makes you create one first.

## What a workflow holds

Since agents are just CLI sessions, workflows are buckets of defualt settings for those agents. For instance, a workflow might indicate which model to use (like opus or haiku), the working directory, or specific actions to run on startup or deletion, relevant for worktree developmemt.

Ultimately, each workflow setting for an agent can be overridden at the moment when an agent is spawned; this approach provides flexibility, for instance when you'd like to use a particular workflow, but with a different model or different directory.

Workflows also hold shared context. Represented as free text, this is appended to the system prompt of every agent created inside the workflow.

Which CLI the agents run is known as the "harness." Currently, `claude` and `codex` are supported. Like all settings, agents already spawned keep the harness they started on, so changing this only affects new ones.

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

Archiving does not run the deprovision action, so whatever was provisioned is still on disk.

Restoration from the archive is all or nothing. It is refused if the returning agents would not fit under your configured agent ceiling.

## Archiving on a finished pull request

A worktree workflow can archive its agents for you. Turn on "Archive once the pull request closes?" in the workflow form, under the worktree settings, and weave watches the pull request each of that workflow's agents recorded. Once one merges or closes, that agent is archived.

Weave checks every couple of minutes, and only archives an agent that is idle or already dormant, so a merge never takes a pane away from an agent mid-turn. An agent that never opened a pull request is never touched, since there is nothing to watch, and neither is one whose recorded pull request turns out to belong to another branch or another repository.

An agent archived this way carries a `pr_archived` metadata key naming the pull request it was archived for. That is what tells you why a row went away, and it is also what stops a restored agent from being archived again two minutes after you brought it back.

Nothing is deleted. The record, the transcript, and the worktree all survive, exactly as if you had archived it by hand, and restoring the agent brings it back. If you want the disk back, delete the agent, which is what runs the deprovision action.

## Setting up your first one

Open the panel with `prefix + g`. Press `w`, give it a name, leave everything else blank, and submit. Press `c` and give an agent a prompt. That is the whole minimum: a workflow with nothing but a name is just a folder.

Then add to it as you learn what the group needs. A default directory once you are tired of typing it. Context once you have told three agents the same thing. A provisioner once you want them out of each other's way. A pre-launch hook once the directory it provisions needs something installed in it.
