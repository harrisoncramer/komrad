# Cookbooks

A cookbook packages a [workflow](workflows.md), the [actions](actions.md) it names, and
optionally a [schedule](schedules.md), into something you install by name instead of typing in
by hand. `weave cookbook list` shows what is available, and `weave cookbook install <name>`
writes the actions into your actions directory and creates the workflow and schedule over the
same endpoints the panel's `w` key and the MCP tools already use. Installing a cookbook is not
a new code path; it drives the existing ones on your behalf.

```sh
weave cookbook list
weave cookbook install worktree-agent --cwd ~/myrepo
```

## The three built in

- **worktree-agent**. One git worktree per agent, created by a setup action and removed by a
  cleanup action, matching the [worked example](workflows.md#a-worked-example-a-worktree-per-agent)
  in the workflows doc. The setup and cleanup scripts read the repository to work in off
  `WEAVE_AGENT_CWD` rather than a hardcoded path, so pass `--cwd` at install time pointing at
  your repository and every agent in the workflow gets its own worktree under
  `<repo>/.worktrees/<agent id>` with no further editing.
- **triage-schedule**. A workflow for an on-call triage agent plus a schedule that fires every
  weekday morning at 9am with the prompt "read the overnight alerts and write up anything that
  needs a human." It installs disabled, since arming a cron job nobody asked for yet is worse
  than a schedule that sits there until you turn it on with `weave rpc set-schedule-enabled`.
  Edit the workflow's context to say what your rotation actually watches, and give it a `--cwd`
  if the agent needs a working directory to look anything up from.
- **pr-reviewer**. A workflow whose context tells the agent it is reviewing an already-open
  pull request rather than authoring one, plus a companion `open-pr` action that opens whatever
  URL the agent recorded on itself under `pr`.

## The manifest

A cookbook is a directory holding a `cookbook.toml` and the action scripts it names as sibling
files. The directory's own name is the cookbook's name, the one `install` takes.

```toml
description = "One line shown by weave cookbook list."

[workflow]
name = "worktree agent"
context = "Whatever should be appended to every agent's system prompt."
harness = ""
cwd = ""
flags = ""
setup_action = "worktree-agent-setup"
cleanup_action = "worktree-agent-cleanup"

[[actions]]
name = "worktree-agent-setup"
file = "setup"

[[actions]]
name = "worktree-agent-cleanup"
file = "cleanup"

[schedule]
name = "morning-triage"
cron = "0 9 * * 1-5"
timezone = ""
prompt = "What the scheduled agent is told."
enabled = false
hide_agents = false
```

`[workflow]` takes the same fields `create_workflow` does. `[[actions]]` is zero or more
scripts to install, each a `name` (what it is written as in your actions directory) and a
`file` (its path relative to the cookbook's own directory). `setup_action` and
`cleanup_action` are plain strings and typically, but not necessarily, name one of the
installed actions. `[schedule]` is optional; when present it is armed against the workflow
`install` just created, so its `workflow_id` is not part of the manifest.

## Where they live

Cookbooks are discovered the same way actions are: a configured directory, and this build's
own built-ins underneath it.

```toml
[cookbooks]
dir = "~/some/other/path"
```

Left unset, your own cookbooks live at `~/.config/weave/cookbooks`, one subdirectory per
cookbook. A cookbook there overrides a built-in of the same name, which is how you replace
`worktree-agent` with your own version once the built-in stops fitting.

## What install does, and does not, do

`install` writes the cookbook's action scripts, creates its workflow, and arms its schedule if
it has one. It does not create an agent. Press `n` on the new workflow, or call `create_agent`,
once you are ready to start one.

A cookbook has no identity of its own once installed; the workflow it created is the only
record. `install` checks the live workflows for one already named what the manifest's
`[workflow]` would create and refuses if it finds one, so running `install` twice does not
leave two indistinguishable workflows behind. Archive or rename the existing one first if you
mean to install a second copy.

A cookbook cannot know your repository path or what your on-call rotation watches in advance,
so `--cwd` overrides the manifest's own working directory at install time, and the workflow's
context and cron expression are yours to edit afterward with `e` on the workflow in the panel,
or `weave rpc update-workflow` and `weave rpc update-schedule`.
