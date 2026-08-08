# Actions

An action is a script or a binary meant to be executed by an agent, you, or automatically on lifecycle events.

Use actions, for instance, to configure or clean up worktrees, or to open a finished agent's pull request in your browser.

## Where they live

Actions are placed into your actions directory, which is at `~/.config/weave/actions` by default, unless your [config](configuration.md) names another:

```toml
[actions]
dir = "~/some/other/path"
```

The directory is read on every call, so an action you write while the service is running shows up without a restart. Actions must be executable. An action runs in the actions directory it was found in.

## What an action is told

Actions get values through the environment, over whatever the service itself is running with.

The `WEAVE_AGENT` environment variable is a JSON blob that carries `id`, `name`, `state`,`cwd`, `harness`, `session_id`, and other information about the agent. Other environment variables are:

- `WEAVE_AGENT_ID` and `WEAVE_AGENT_CWD`, the two fields most scripts want.
- `WEAVE_ADDR`, this server's address, for calling back into the API or the [CLI](commands.md).
- `WEAVE_TMUX_SOCKET`, the tmux server every agent's pane lives on
- `WEAVE_ACTIONS_DIR`, the directory the action came out of, which is how a script that cds elsewhere finds its siblings again.
- `WEAVE_TRIGGER`, what set the action off. One of `manual`, `setup`, `resume`, or `cleanup`.
- `WEAVE_ACTION`, this action's own name, for tagging its own log lines.
- `WEAVE_INFO_PANE`, the panel's info pane, for a script that wants to draw something where the panel's own actions do.
- `WEAVE_HOST_TMUX_SOCKET` and `WEAVE_HOST_CLIENT`, your own tmux server and the client looking at the popup.
- `EDITOR` and `WEAVE_SHELL`

## Action Metadata

You can configure a small set of metadata for an action by adding _headers_ to the top of the file.

```bash
#!/bin/sh
#WEAVE description="This is my custom action"
#WEAVE timeout="1s"

# Add whatever logic you want here.
# Failures on stdout will get captured by weave's log automatically
# You can also send messages to the log manually, like this:
weave rpc log --message 'Hello world' --action "$WEAVE_ACTION"
```

This action will abort and fail if it cannot execute within 1 second.

## The three ways an action runs

### As a workflow's setup action

Every [workflow](workflows.md) can name one action to run before each of its agents starts. This is the one place where the action is synchronous, as the launch waits for it.

The same action runs with `WEAVE_TRIGGER=resume` if an agent is resumed later, such as when an agent is unarchived and restored.

### As a workflow's cleanup action

A workflow can also name an action to run once one of its agents is deleted. Nothing waits for it, so the call returns as soon as the process exists.

Cleanup fires on delete, and on a failed launch. It does not fire on archive.

### On demand

Press `a` on an agent in the panel to see every action (except for those prefixed with `_`), and press `enter` to fire the one under the cursor.

You can also interact with actions via the [MCP](mcp.md) tools, `list_actions` and `run_action`, and via the CLI:

```sh
weave rpc list-actions
weave rpc run-action --agent-id <id> --action open-pr
```

To report progress on purpose, from an action or from anywhere else acting on an agent's behalf, there is also `log`:

```sh
weave rpc log --message 'Some custom message'
```

Agents can also run actions, for instance to trigger external system, fire alerts, or manage the filesystem.

## Example: create a worktree

Create an executable file called `create-worktree`:

```sh
#!/bin/sh
set -eu

repo="$HOME/code/weave"
name=$(printf '%s' "$WEAVE_AGENT" | jq -r .name)
tree="$repo/.worktrees/$name"

weave rpc log --message "Preparing $tree" --action "$WEAVE_ACTION"

if [ ! -d "$tree" ]; then
	git -C "$repo" fetch --quiet origin main
	git -C "$repo" worktree add --quiet -B "agent/$name" "$tree" origin/main
fi

weave rpc log --message "Prepared $tree" --action "$WEAVE_ACTION"
```

In the workflow manager, set it as a workflow's setup action.

Every agent spawned under that workflow will now get a new worktree. This matching cleanup action called `cleanup-worktree` can be used to remove it when the agent is deleted:

```sh
#!/bin/sh
set -eu

repo="$HOME/code/weave"
tree="$WEAVE_AGENT_CWD"

case "$tree" in
"$repo/.worktrees/"*) git -C "$repo" worktree remove --force "$tree" ;;
*) weave rpc log --message "Failed to prepare $tree" --action "$WEAVE_ACTION"
esac
```

## Example: open the pull request an agent recorded

Agents can be instructed to record their own work as metadata via the MCP, such as the URL of a pull request. This action reads it back out of the payload and opens it.

Open the agent manager and press `a` to view your actions, then select it:

```sh
#!/bin/sh
set -eu

url=$(printf '%s' "$WEAVE_AGENT" | jq -r '.metadata.pr // empty')

if [ -z "$url" ]; then
	# Nothing is watching stdout, so say it where the person is looking.
	tmux -L "$WEAVE_HOST_TMUX_SOCKET" display-message \
		-c "$WEAVE_HOST_CLIENT" "no pr recorded on this agent"
	exit 1
fi

exec open "$url"
```

## The builtins, and replacing one

Weave ships one action by default, `_edit_action`, which is what `e` runs on the actions list: it opens the script under the cursor in your editor so you can change it without leaving weave. It's told which file to open in `WEAVE_EDIT_PATH`, since the name you pressed the key on can come from either your own directory or the one weave ships. A file with no text in it is not offered, since there is nothing in a compiled program to edit.

Action names starting with underscores like these are left out of the panel's action list. Add your own `_edit_action` to replace the builtin one.


