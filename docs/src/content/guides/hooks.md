---
title: Hooks
description: Your own scripts, run by komrade at moments in an agent's life.
group: concepts
order: 5
---

A hook is your own script that komrade runs at a particular moment in an agent's life. Hooks cover the setup and cleanup komrade cannot know about: installing dependencies in a fresh worktree, starting a service the agent needs, or tearing down what an agent started once it is deleted.

## Where hooks live

Hooks are ordinary executable files in your hooks directory, set by [`hooks.dir`](../../config/#hooks.dir). A workflow picks which hook runs at which moment, so the same script can serve many workflows. Each hook runs inside its agent's directory, which is the agent's worktree when the workflow gives it one.

## The moments

Before launch runs once the agent's directory exists but before its CLI starts. It is the place for dependency installs. Komrade waits for it, and if it fails the agent is not started. The agent stays in the list marked as failed, its worktree is given back, and you can retry once the cause is fixed.

After launch runs once the CLI is up. After resume runs when a stopped agent comes back. After restore runs when an archived agent is brought back. These three run in the background, and komrade does not wait for them.

Before teardown runs when an agent is deleted, before its worktree is removed. Use it to take down anything the agent started outside its own directory, such as a docker stack. Komrade waits for it. If it fails, the delete stops and the worktree is kept, so a later attempt still has everything it needs. Because a delete may be retried, write this hook so it is safe to run twice. Archiving an agent does not run it, because archiving keeps the worktree.

Child agents and agents working in someone else's checkout run no hooks, because the directory they are in is not theirs.

## What a hook knows

Komrade tells a hook about its agent through environment variables: which agent it is, its directory, what fired the hook, and where to reach komrade's API. A hook in a worktree workflow is also told about the repository and the base branch. `komrade` itself is on every hook's path, so a hook can ask the API anything else, such as the metadata an agent has recorded.

Git inside a hook never stops to ask for a password, so a hook with expired credentials fails instead of hanging.

A hook can declare a description and a timeout in comment lines at the top of the file. A hook that runs past its timeout is stopped and counts as failed.

## Seeing what a hook did

Every run of every hook writes its own log, and nothing overwrites an earlier one. The panel can show an agent's hook logs beside its terminal, starting with a failed run if there is one. When a hook fails, the error komrade reports includes the end of its output and where the full log is.

<figure class="placeholder">
  <figcaption>An agent whose before-launch hook failed, with the hook's log open in the panel's info pane.</figcaption>
</figure>

A hook, or anything else acting for an agent, can also write a line into komrade's own log on purpose with `komrade rpc log`, which is handy for reporting progress.
