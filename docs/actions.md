# Actions

An action is a script or a binary meant to be executed by an agent, you, or automatically on lifecycle events.

Use actions, for instance, to configure or clean up worktrees, or to open a finished agent's pull request in your browser.

## Where they live

Actions are placed into your actions directory, which is at `~/.config/weave/actions` by default, unless your [config](configuration.md) names another:

```toml
[actions]
dir = "~/some/other/path"
```

An action runs in the actions directory it was found in.

## Execution

Actions run at three different moments.

### As a workflow's setup action

Every [workflow](workflows.md) can name one action to run before each of its agents starts. This is the one place where the action is synchronous, as the launch waits for it.

The same action runs with `WEAVE_TRIGGER=resume` if an agent is resumed later, such as when an agent is unarchived and restored.

If it exits zero, weave reads `WEAVE_SETUP_DIR_FILE`, trims it, and uses that as the directory the agent runs in. Write nothing and the agent keeps the directory the workflow or the form chose. This is a file rather than stdout, so a script's own output, and whatever it shells out to, never gets mistaken for the answer:

```sh
#!/bin/sh
set -eu

tree="$HOME/trees/$WEAVE_AGENT_ID"
echo "creating a worktree at $tree" # fine, this is just output

printf '%s' "$tree" >"$WEAVE_SETUP_DIR_FILE"
```

### As a workflow's cleanup action

A workflow can also name an action to run once one of its agents is deleted. Nothing waits for it, so the call returns as soon as the process exists.

Cleanup fires on delete, and on a failed launch. It does not fire on archive.

### On demand

Press `x` on an agent in the panel to see every action (except for those prefixed with `_`), and press `enter` to fire the one under the cursor.

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

## The Environment

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

Every action also runs with `GIT_TERMINAL_PROMPT=0` and `GCM_INTERACTIVE=never`, and with `GIT_ASKPASS` and `SSH_ASKPASS` forced empty, so a git call against expired credentials fails rather than opening a prompt nothing answers. A non-interactive askpass you already configured is left as you set it.

## Action Metadata

You can configure a small set of metadata for an action by adding _headers_ to the top of the file.

```bash
#!/bin/sh
#WEAVE description="This is my custom action"
#WEAVE timeout="1s"

# Add whatever logic you want here.
# What this writes to stdout or stderr is captured, kept in full in weave's log and
# the per-agent action log, and cut to a few hundred bytes in a setup refusal's banner.
# You can also send messages to the log manually, like this:
weave rpc log --message 'Hello world' --action "$WEAVE_ACTION"
```

This action will abort and fail if it cannot execute within 1 second.

## Example: open the pull request an agent recorded

Agents can be instructed to record their own work as metadata via the MCP, such as the URL of a pull request. Weave ships this one as `_open_pr`, bound to `o` in the agents list on the agents that have one recorded. It reads the value back through the API rather than out of the payload, which is what any action of your own wanting one field should do, since `weave` is on an action's PATH:

```sh
#!/bin/sh
set -eu

agent=$(weave rpc get-agent --id "$WEAVE_AGENT_ID" --compact)
url=$(printf '%s' "$agent" | jq -r '[.agent.metadata[]? | select(.key == "pr") | .value] | first // ""')

exec open "$url"
```

## The builtins, and replacing one

Weave ships some actions by default.

Action names starting with underscores like these are left out of the panel's action list. Add your own to override the builtins.

`_worktree_setup` and `_worktree_cleanup` back a workflow's `worktree.enabled` toggle. Setup copies or symlinks whatever `worktree.copy_paths` and `worktree.symlink_paths` name, skipping any path git already tracks, since the fresh checkout already has it.

An agent that [adopted](agents.md) a branch arrives at setup with `WEAVE_WORKTREE_ADOPT_BRANCH` set, and the worktree is made on that branch instead of a new one cut from the base. Cleanup reads the same variable and leaves the branch where it is, since it was never weave's to delete.

