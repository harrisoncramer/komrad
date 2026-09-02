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

Actions run at four different moments.

### As a workflow's provision action

Every [workflow](workflows.md) can name one action that owns where each of its agents runs. This is the one place where the action is synchronous, as the launch waits for it, and a failure abandons the create.

The same action runs with `WEAVE_TRIGGER=resume` if an agent is resumed later, since a dormant agent's directory is not guaranteed to still be there and the provisioner is the only thing that can put it back and say where it is.

If it exits zero, weave reads `WEAVE_SETUP_DIR_FILE`, trims it, and uses that as the directory the agent runs in. Write nothing and the agent keeps the directory the workflow or the form chose. This is a file rather than stdout, so a script's own output, and whatever it shells out to, never gets mistaken for the answer:

```sh
#!/bin/sh
set -eu

tree="$HOME/trees/$WEAVE_AGENT_ID"
echo "creating a worktree at $tree" # fine, this is just output

printf '%s' "$tree" >"$WEAVE_SETUP_DIR_FILE"
```

### As a workflow's deprovision action

A workflow can also name the other half of its provisioner, which gives that directory back once one of its agents is deleted. Nothing waits for it, so the call returns as soon as the process exists, except on a failed launch, where it is awaited so the create cannot report having failed while the directory is still being taken back.

Deprovision fires on delete, and on a failed launch. It does not fire on archive, and it is not read at all for an agent whose own provision action is blank, since such an agent keeps the directory it was given and has nothing to give back.

### As a workflow's hook

A workflow can also hang scripts on a moment in its agents' lives rather than on their directories. A moment can hold several, they run in the order they are listed, and they never name a directory of their own.

`pre-launch` runs after the directory exists and before the pane is opened, which is where a dependency install goes. Weave waits for each one, and a failure abandons the create, since there is no agent yet: the hooks after it are skipped and the provisioner's other half runs so the directory does not survive a create that did not happen.

`post-launch` runs once the CLI has been launched into the pane. Weave still waits for one before starting the next, but the chain runs in the background, so the call that created the agent returns straight away and a hook that fails there is reported rather than undone.

`post-resume` runs once a dormant agent is back in a pane, after the provisioner has rerun, and `post-restore` runs when an archived row comes back. Both run in the background like `post-launch`, and a failure is logged rather than undone.

A hook runs in the agent's own directory, which is the one the provisioner named, and reads the moment out of `WEAVE_TRIGGER` like any other action. A child agent runs no hooks at all, since it is spawned into a directory its parent already prepared.

### On demand

Fire an action on an agent through the [MCP](mcp.md) tools, `list_actions` and `run_action`, or through the CLI:

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
- `WEAVE_TRIGGER`, what set the action off. One of `manual`, `provision`, `resume`, `deprovision`, `pre-launch`, `post-launch`, `post-resume`, or `post-restore`.
- `WEAVE_ACTION`, this action's own name, for tagging its own log lines.
- `WEAVE_INFO_PANE`, the panel's info pane, for a script that wants to draw something where the panel's own actions do.
- `WEAVE_HOST_TMUX_SOCKET` and `WEAVE_HOST_CLIENT`, your own tmux server and the client looking at the popup.
- `EDITOR` and `WEAVE_SHELL`

Every action also runs with `GIT_TERMINAL_PROMPT=0` and `GCM_INTERACTIVE=never`, and with `GIT_ASKPASS` and `SSH_ASKPASS` forced empty, so a git call against expired credentials fails rather than opening a prompt nothing answers. A non-interactive askpass you already configured is left as you set it.

## Where a run's output is kept

Every run of an action writes a file of its own under the cache directory, numbered so the runs read newest first:

    weave-action-logs/<agent-id>/001-provision-_worktree_setup.log
                                 002-pre-launch-install-deps.log
                                 003-manual-open-pr.log

The number counts up per agent and the name says what set the run off, so nothing overwrites anything: an agent resumed twice has three provision runs to compare rather than one file that was written over. Only the last fifty runs are kept, and they all go when the agent is deleted.

In the panel the logs key opens the most recent run in the info pane, and `L` steps back through the runs before it, wrapping round to the most recent. An agent that failed to be created, or failed to be torn down, points at the run that actually failed rather than guessing between two files.

## Action Metadata

You can configure a small set of metadata for an action by adding _headers_ to the top of the file.

```bash
#!/bin/sh
#WEAVE description="This is my custom action"
#WEAVE timeout="1s"

# Add whatever logic you want here.
# What this writes to stdout or stderr is captured, kept in full in weave's log and
# the per-agent action log, and cut to a few hundred bytes in a provision refusal's banner.
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

Action names starting with underscores like these are left out of the workflow form's provision and deprovision fields. Add your own to override the builtins.

`_worktree_setup` and `_worktree_cleanup` are the provisioner behind a workflow's `worktree.enabled` toggle, and are reached only by turning it on rather than by naming them in a field. The setup half copies or symlinks whatever `worktree.copy_paths` and `worktree.symlink_paths` name, skipping any path git already tracks, since the fresh checkout already has it. A branch the agent already has is checked out as it stands rather than reset onto the base, so a directory removed by hand and made again keeps whatever the agent had committed.

An agent that [adopted](agents.md) a branch arrives at provision with `WEAVE_WORKTREE_ADOPT_BRANCH` set, and the worktree is made on that branch instead of a new one cut from the base. The cleanup half reads the same variable and leaves the branch where it is, since it was never weave's to delete.

