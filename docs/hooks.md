# Hooks

A hook is a script or a binary weave runs at a moment in an agent's life. A [workflow](workflows.md) names one against a moment, and weave runs it with the agent's details in its environment.

Use hooks to install what a fresh worktree needs, to copy an env file into it, to announce an agent in Slack once it is up, or to release a lock when one comes back from the archive.

Where an agent runs is not a hook's business. That belongs to the workflow's provisioner, which is either a git worktree per agent or nothing at all, and weave owns both: there is no script of yours in that decision.

## Where they live

Hooks are placed into your hooks directory, which is at `~/.config/weave/hooks` by default, unless your [config](configuration.md) names another:

```toml
[hooks]
dir = "~/some/other/path"
```

A hook runs in the agent's own directory, which is the worktree when its workflow provisions one. A hook whose directory has gone is refused rather than run somewhere else.

## The moments

A workflow can hang one hook on each of these, picked in the workflow form from the hooks directory.

`pre-launch` runs after the directory exists and before the pane is opened, which is where a dependency install goes. Weave waits for it, and a failure abandons the create: there is no agent yet, and the worktree is given back so one does not survive a create that did not happen.

`post-launch` runs once the CLI has been launched into the pane. It runs in the background, so the call that created the agent returns straight away and a hook that fails there is reported rather than undone.

`post-resume` runs once a dormant agent is back in a pane, after its worktree has been put back. `post-restore` runs when an archived row comes back, whether it was restored on its own or with the workflow it sits in. Both run in the background like `post-launch`.

A child agent runs no hooks at all, and neither does an agent told to keep the directory it was given, since both are working in a checkout somebody else prepared and a hook there would be writing into a tree nobody asked it to touch.

## The environment

A hook gets its values through the environment, over whatever the service itself is running with. The scripts weave ships get the same ones.

The `WEAVE_AGENT` environment variable is a JSON blob that carries `id`, `name`, `state`,`cwd`, `harness`, `session_id`, and other information about the agent. Other environment variables are:

- `WEAVE_AGENT_ID` and `WEAVE_AGENT_CWD`, the two fields most scripts want.
- `WEAVE_ADDR`, this server's address, for calling back into the API or the [CLI](commands.md).
- `WEAVE_TMUX_SOCKET`, the tmux server every agent's pane lives on
- `WEAVE_SCRIPT_DIR`, the directory the script came out of, which is how one that cds elsewhere finds its siblings again.
- `WEAVE_TRIGGER`, the moment that set it off. One of `pre-launch`, `post-launch`, `post-resume`, `post-restore`, or, for the scripts weave ships, `provision`, `resume`, `deprovision`, or `open-pr`.
- `WEAVE_SCRIPT`, its own name, for tagging its own log lines.
- `WEAVE_INFO_PANE`, the panel's info pane, for a script that wants to draw something where the panel's own scripts do.
- `WEAVE_HOST_TMUX_SOCKET` and `WEAVE_HOST_CLIENT`, your own tmux server and the client looking at the popup.
- `EDITOR` and `WEAVE_SHELL`

Every script also runs with `GIT_TERMINAL_PROMPT=0` and `GCM_INTERACTIVE=never`, and with `GIT_ASKPASS` and `SSH_ASKPASS` forced empty, so a git call against expired credentials fails rather than opening a prompt nothing answers. A non-interactive askpass you already configured is left as you set it.

## Where a run's output is kept

Every run writes a file of its own under the cache directory, numbered so the runs read newest first:

    weave-run-logs/<agent-id>/001-provision-_worktree_setup.log
                              002-pre-launch-install-deps.log
                              003-post-launch-announce.log

The number counts up per agent and the name says what set the run off, so nothing overwrites anything: an agent resumed twice has three provision runs to compare rather than one file that was written over. Only the last fifty runs are kept, and they all go when the agent is deleted.

In the panel the logs key opens the most recent run in the info pane, and `L` steps back through the runs before it, wrapping round to the most recent. An agent that failed to be created, or failed to be torn down, points at the run that actually failed rather than guessing between two files.

## Hook metadata

You can configure a small set of metadata for a hook by adding _headers_ to the top of the file.

```bash
#!/bin/sh
#WEAVE description="This is my hook"
#WEAVE timeout="1s"

# Add whatever logic you want here.
# What this writes to stdout or stderr is captured, kept in full in weave's log and
# the per-agent run log, and cut to a few hundred bytes in a refusal's banner.
# You can also send messages to the log manually, like this:
weave rpc log --message 'Hello world' --script "$WEAVE_SCRIPT"
```

This hook will abort and fail if it cannot execute within 1 second.

To report progress on purpose, from a hook or from anywhere else acting on an agent's behalf, there is also `log`:

```sh
weave rpc log --message 'Some custom message'
```

## Example: reading one field back

Agents can be instructed to record their own work as metadata via the MCP, such as the URL of a pull request. Weave ships a script that opens it, `_open_pr`, bound to `o` in the agents list on the agents that have one recorded. It reads the value back through the API rather than out of the payload, which is what a hook of your own wanting one field should do too, since `weave` is on the PATH of every script weave runs:

```sh
#!/bin/sh
set -eu

agent=$(weave rpc get-agent --id "$WEAVE_AGENT_ID" --compact)
url=$(printf '%s' "$agent" | jq -r '[.agent.metadata[]? | select(.key == "pr") | .value] | first // ""')

exec open "$url"
```

## The scripts weave ships

Weave ships three scripts of its own, in a directory of its own that nothing configures. They are weave's implementation rather than anything to name or replace, and they are shell today only because that is what they were written in.

`_worktree_setup` and `_worktree_cleanup` are the provisioner behind a workflow's `worktree.enabled` toggle, and are reached only by turning it on. The setup half copies or symlinks whatever `worktree.copy_paths` and `worktree.symlink_paths` name, skipping any path git already tracks, since the fresh checkout already has it. A branch the agent already has is checked out as it stands rather than reset onto the base, so a directory removed by hand and made again keeps whatever the agent had committed.

An agent that [adopted](agents.md) a branch arrives at provision with `WEAVE_WORKTREE_ADOPT_BRANCH` set, and the worktree is made on that branch instead of a new one cut from the base. The cleanup half reads the same variable and leaves the branch where it is, since it was never weave's to delete.

