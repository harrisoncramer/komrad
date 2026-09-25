---
title: Hooks
description: Your own scripts, run by komrad at moments in an agent's life.
group: start
order: 4
---

Your [agent](../agents/) started in a clean worktree, so the first thing it did was probably fail to run the tests. A hook is your own script that komrad runs at a moment in an agent's life, and this is the moment for one. Put an executable file called `install` in your hooks directory containing `npm install`, then press `e` on the [workflow](../workflows/) and pick it as the before-launch hook. Every agent spawned from now on gets its dependencies before its CLI starts.

Hooks cover the setup and cleanup komrad cannot know about, like that install, or tearing down a docker stack an agent started.

## Where hooks live

Hooks are executable files in [`hooks.dir`](../../config/#hooks.dir). A workflow picks which hook runs at which moment, so one script can serve many workflows. Each hook runs inside its agent's directory.

## The moments

Before launch runs once the agent's directory exists but before its CLI starts. Komrad waits for it. If it fails, the agent is not started and stays in the list marked as failed, so you can fix the cause and retry.

After launch, after resume and after restore run in the background once the CLI is up. Komrad does not wait for them.

Before teardown runs when an agent is deleted, before its worktree is removed. Komrad waits for it, and if it fails the delete stops and the worktree is kept. Write it so it is safe to run twice. Archiving does not run it.

Child agents run no hooks, because the directory they are in is not theirs.

## What a hook knows

Komrad tells a hook which agent it is, its directory, what fired the hook, and where to reach the API, through environment variables. `komrad` is on the hook's path, so it can ask the API anything else. A hook can declare a description and a timeout in comment lines at the top.

## Seeing what a hook did

Every run writes its own log. The panel can show an agent's hook logs beside its terminal, and when a hook fails, the error includes the end of its output and where the full log is.

<figure class="placeholder">
  <figcaption>An agent whose before-launch hook failed, with the hook's log open in the panel's info pane.</figcaption>
</figure>

Agents you start by hand are one thing. A [schedule](../schedules/) starts them for you.
