# Hooks

A hook is a script or a binary weave runs at a moment in an agent's life spawned by a workflow.

Use a hook to install packages or tooling in a worktree, or to do setup the workflow's copy and symlink settings cannot cover. A hook can also clean up after an agent is deleted, or run when one comes back out of the archive.

## Where they live

Hooks are scripts that should be placed into your hooks directory, at `~/.config/weave/hooks` by default, unless your [config](../config.toml) names another:

```toml
[hooks]
dir = "~/some/other/path"
```

Hooks run in their agent's directory, which is the worktree when its workflow provisions one.

## The moments

Every workflow can add one hook on each of these lifecycle events, picked in the workflow:

- `pre-launch` runs after the agent's directory exists but before the pane is opened. Good for dependency installs (that aren't covered by the workflows copy/symlinks), for instance. A failure here is blocking: no agent is spawned, and any worktree created is given back.
- `post-launch` runs once the CLI has been launched into the pane. It runs in the background.
- `post-resume` runs once a dormant agent is back in a pane, after its worktree has been put back. Runs in the background.
- `post-restore` runs when an archived row comes back. Runs in the background.
- `pre-teardown` runs when an agent is deleted, before its worktree is handed back. This is where you take down anything the agent started outside its own directory, such as a docker stack it brought up from its pane. Weave waits for this one. If it fails the delete stops, the row goes to `failed_to_cleanup`, and the worktree is left alone so the next attempt still has the files it needs. A delete can therefore ask this hook to run more than once, so write one that is safe to run again. Weave skips it if the worktree has already gone, because there is nothing left for it to work on. It does not run when you archive an agent. Archiving keeps the worktree, and restoring the agent puts it back to work in the same one.

Child agents do not run hooks. Neither do agents told to keep the directory they were given instead of being made one. Both may be working in a directory that belongs to somebody else.

## The environment

Hooks get their values through the environment.

The `WEAVE_AGENT` environment variable is a JSON blob that carries `id`, `name`, `state`,`cwd`, `harness`, `session_id`, and other information about the agent that fired the hook. Other environment variables are:

- `WEAVE_AGENT_ID` and `WEAVE_AGENT_CWD`, the two fields most scripts want.
- `WEAVE_ADDR`, this server's address, for calling back into the API or the [CLI](commands.md).
- `WEAVE_TMUX_SOCKET`, the tmux server every agent's pane lives on
- `WEAVE_SCRIPT_DIR`, the directory the script came out of, which is how one that cds elsewhere finds its siblings again.
- `WEAVE_TRIGGER`, the moment that set it off. One of `pre-launch`, `post-launch`, `post-resume`, `post-restore`, `pre-teardown`, or, for the scripts weave ships, `provision`, `resume`, or `deprovision`.
- `WEAVE_SCRIPT`, its own name, for tagging its own log lines.
- `WEAVE_INFO_PANE`, the panel's info pane, for a script that wants to draw something where the panel's own scripts do.
- `WEAVE_HOST_TMUX_SOCKET`, your own tmux server, as the last open recorded it.
- `EDITOR` and `WEAVE_SHELL`

Every script also runs with `GIT_TERMINAL_PROMPT=0` and `GCM_INTERACTIVE=never`, and with `GIT_ASKPASS` and `SSH_ASKPASS` forced empty, so a git call against expired credentials fails without prompting.

## Hook output

Every run writes a file of its own under weave's cache directory, numbered so the runs read newest first:

    weave-run-logs/<agent-id>/001-provision-_worktree_setup.log
                              002-pre-launch-install-deps.log
                              003-post-launch-announce.log

Nothing overwrites anything else, so that an agent resumed twice has three provision run logs.

In weave's panel, the "logs" key opens the most recent run in the info pane (the `l` key by default) and `L` steps back through previous run logs of the same hook type.

## Hook metadata

You can configure a small set of metadata for a hook by adding _headers_ to the top of the file.

```bash
#!/bin/sh
#WEAVE description="This is my hook"
#WEAVE timeout="1s"

# Add whatever logic you want here.
# What this writes to stdout or stderr is captured, kept in full in weave's log and
# the per-agent run log, and cut to a few hundred bytes in the message a refusal is reported as.
# You can also send messages to the log manually, like this:
weave rpc log --message 'Hello world' --script "$WEAVE_SCRIPT"
```

This hook will abort and fail if it cannot execute within 1 second.

To report progress on purpose, from a hook or from anywhere else acting on an agent's behalf, use the `log` CLI command:

```sh
weave rpc log --message 'Some custom message'
```

## Example: reading one field back

Agents can be instructed to record their own work as metadata via the MCP, such as the ticket they are implementing. A hook that wants one such field should read it back through the API rather than out of the payload, since `weave` is on the PATH of every script weave runs:

```sh
#!/bin/sh
set -eu

agent=$(weave rpc get-agent --id "$WEAVE_AGENT_ID" --compact)
url=$(printf '%s' "$agent" | jq -r '[.agent.metadata[]? | select(.key == "ticket") | .value] | first // ""')

exec open "$url"
```
