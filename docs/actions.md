# Actions


An action is a script or a binary, meant to be executed by an agent, on it's behalf by you, or automatically on certain lifecycle events.

Nothing about an action is specific to weave. It's just an executable file, which gets told which agent it's running for through the `WEAVE_AGENT` environment variable.

Use actions, for instance, to configure or clean up worktrees, or to open a finished agent's pull request in your browser.

## Where they live

Actions are placed into your actions directory, which is at `~/.config/weave/actions` by default, unless your [config](configuration.md) names another:

```toml
[actions]
dir = "~/some/other/path"
```

The directory is read on every call, so an action you write while the service is running shows up without a restart. Actions must be executable. Files that aren't will be listed in the manager panel, but not execute.

## What an action is told

An action gets no arguments. Everything arrives in the environment, over whatever the service itself is running with.

The `WEAVE_AGENT` environment variable is a JSON blob that carries `id`, `name`, `state`,`cwd`, `harness`, `session_id`, and other information about the agent. Other environment variables are:

- `WEAVE_AGENT_ID` and `WEAVE_AGENT_CWD`, the two fields most scripts want.
- `WEAVE_ADDR`, this server's address, for calling back into the API or the [CLI](commands.md).
- `WEAVE_TMUX_SOCKET`, the tmux server every agent's pane lives on. Send keys to the pane in the payload through this one, not through your own server, where that pane id means something else entirely.
- `WEAVE_ACTIONS_DIR`, the directory the action came out of, which is how a script that cds elsewhere finds its siblings again.
- `WEAVE_TRIGGER`, what set the action off. One of `manual`, `setup`, `resume`, or `cleanup`.
- `WEAVE_SECONDARY_PANE`, the pane under the panel's stage, for a script that wants to draw something where the panel's own actions do.
- `WEAVE_HOST_TMUX_SOCKET` and `WEAVE_HOST_CLIENT`, your own tmux server and the client looking at the popup, for a script that wants to open a window or say something where you'll see it.
- `EDITOR` and `WEAVE_SHELL`.

An action runs in the actions directory it was found in, not in the agent's directory, and stdin and stdout go to the null device. To communicate back to weave, print on stderr: the last 64 KiB of it is captured and written to weave's log.

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

Agents can also run actions, for instance to trigger external system, fire alerts, or manage the filesystem.

## Example: create a worktree for each agent

Create an executable file called `create-worktree`:

```sh
#!/bin/sh
set -eu

repo="$HOME/code/weave"
name=$(printf '%s' "$WEAVE_AGENT" | jq -r .name)
tree="$repo/.worktrees/$name"

echo "preparing $tree" >&2

# Skipped when the tree is already there, which is what makes this safe to
# reach again on resume.
if [ ! -d "$tree" ]; then
	git -C "$repo" fetch --quiet origin main
	git -C "$repo" worktree add --quiet -B "agent/$name" "$tree" origin/main
fi

# The only thing on stdout: where the agent runs.
printf '%s' "$tree"
```

In the workflow manager, set it as a workflow's setup action. Every agent spawned under that workflow will now get a new worktree. This matching cleanup action called `cleanup-worktree` can be used to remove it when the agent is deleted:

```sh
#!/bin/sh
set -eu

repo="$HOME/code/weave"
tree="$WEAVE_AGENT_CWD"

case "$tree" in
"$repo/.worktrees/"*) git -C "$repo" worktree remove --force "$tree" ;;
*) echo "refusing to remove $tree" >&2 ; exit 1 ;;
esac
```

## Example: open the pull request an agent recorded

Agents can be instructed to record their own work as metadata via the MCP, such as the URL of a pull request. This action reads it back out of the payload and opens it.

Open the agent manager and press `a` to view your actions, then select it:

```sh
#!/bin/sh
set -eu

# The service may be older than this shell, so don't trust its PATH.
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

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

Weave ships some actions by default: `_editor` and `_terminal`. They're what the panel's `e` and `t` keys run.

To provide a custom terminal or edtior over the builtin one, add your own `_editor` action. Action names starting with underscores like these are left out of the panel's action list.


