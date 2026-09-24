---
title: Hooks
description: Scripts weave runs at moments in an agent's life.
group: concepts
order: 5
---

A hook is a script or binary that weave runs at a set moment in an agent's life. Use one to install dependencies in a new worktree, to clean up after an agent is deleted, or to do setup that a workflow's own settings cannot.

## Where hooks live

Put hook scripts in your hooks directory, which is `~/.config/weave/hooks` unless your [config](../../config/) names another.

```toml
[hooks]
dir = "~/some/other/path"
```

A hook runs in its agent's directory. When the workflow gives each agent a worktree, that is the worktree.

## When hooks run

Each workflow can pick one hook for each of these moments.

`pre-launch` runs after the agent's directory exists and before its pane opens. It suits dependency installs. If it fails, the agent is not spawned and any worktree made for it is given back.

`post-launch` runs in the background once the CLI has started in the pane.

`post-resume` runs in the background once a dormant agent is back in a pane and its worktree is restored.

`post-restore` runs in the background when an archived agent comes back.

`pre-teardown` runs when an agent is deleted, before its worktree is removed. Use it to take down anything the agent started outside its own directory, such as a docker stack. Weave waits for it. If it fails, the delete stops, the agent is marked `failed_to_cleanup`, and the worktree is kept so the next attempt still has its files. A delete can run it more than once, so make it safe to repeat. Weave skips it if the worktree is already gone. Archiving does not run it, because archiving keeps the worktree.

Child agents do not run hooks. Neither do agents told to keep the directory they were given. Both may be working in someone else's directory.

## What a hook is told

Hooks get their inputs from the environment. `WEAVE_AGENT` is a JSON object describing the agent that fired the hook, including its `id`, `name`, `state`, `cwd`, `harness` and `session_id`. Most scripts only need `WEAVE_AGENT_ID` and `WEAVE_AGENT_CWD`, which are set on their own.

These are also set:

- `WEAVE_ADDR` is the service's address, for calling back into the API or the CLI.
- `WEAVE_TMUX_SOCKET` is the tmux server every agent's pane lives on.
- `WEAVE_SCRIPT_DIR` is the directory the script came from, so a script that changes directory can still find its siblings.
- `WEAVE_TRIGGER` is the moment that fired it: `pre-launch`, `post-launch`, `post-resume`, `post-restore` or `pre-teardown`. The scripts weave ships see `provision`, `resume` or `deprovision`.
- `WEAVE_SCRIPT` is the script's own name, for tagging its log lines.
- `WEAVE_INFO_PANE` is the panel's info pane, for a script that wants to draw there.
- `WEAVE_HOST_TMUX_SOCKET` is your own tmux server, as last recorded.
- `EDITOR` and `WEAVE_SHELL`.

Every script runs with `GIT_TERMINAL_PROMPT=0` and `GCM_INTERACTIVE=never`, and with `GIT_ASKPASS` and `SSH_ASKPASS` empty. A git call with expired credentials then fails instead of waiting at a prompt.

## Hook output

Each run writes its own numbered log file under weave's cache directory, so nothing overwrites anything.

```
weave-run-logs/<agent-id>/001-provision-_worktree_setup.log
                          002-pre-launch-install-deps.log
                          003-post-launch-announce.log
```

In the panel, `l` opens the latest run's log in the info pane, and `L` steps back through earlier runs of the same hook.

## Hook settings

A hook can set a description and a timeout with header lines at the top of the file.

```bash
#!/bin/sh
#WEAVE description="Install dependencies"
#WEAVE timeout="5m"

npm ci
weave rpc log --message 'dependencies installed' --script "$WEAVE_SCRIPT"
```

This hook fails if it has not finished within five minutes. What it writes to stdout and stderr is kept in full in weave's log and the run log. If it fails, the first few hundred bytes appear in the error.

`weave rpc log --message` writes a line to weave's log on purpose, from a hook or anything else acting for an agent.

## Example: reading a field back

Agents can record facts about their work as metadata, such as the ticket they are on. A hook that wants one should read it from the API, since `weave` is on the `PATH` of every script weave runs.

```sh
#!/bin/sh
set -eu

agent=$(weave rpc get-agent --id "$WEAVE_AGENT_ID" --compact)
url=$(printf '%s' "$agent" | jq -r '[.agent.agent.metadata[]? | select(.key == "ticket") | .value] | first // ""')

exec open "$url"
```
