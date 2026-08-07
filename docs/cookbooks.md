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
  pull request rather than authoring one, plus a companion `pr-reviewer-open-pr` action that
  opens whatever URL the agent recorded on itself under `pr`.

## The manifest

A cookbook is a directory holding a `cookbook.toml` and whatever action scripts it needs as
sibling files. The directory's own name is the cookbook's name, the one `install` takes, and
everything else that would otherwise need a name of its own takes that same name rather than
spelling out a second one that could drift from it: the workflow `install` creates, the
schedule it arms, and every installed action are all named from the cookbook's own name.

```toml
description = "One line shown by weave cookbook list."

[workflow]
context = "Whatever should be appended to every agent's system prompt."
harness = ""
cwd = ""
flags = ""
setup_action = "setup"
cleanup_action = "cleanup"

[schedule]
cron = "0 9 * * 1-5"
timezone = ""
prompt = "What the scheduled agent is told."
enabled = false
hide_agents = false
```

There is nothing to list under `actions`: every file next to `cookbook.toml`, other than the
manifest itself, is installed, the same way your actions directory itself works, where a
script sitting there is enough. Each is written into your actions directory as
`<cookbook name>-<file name>`, so a file named `setup` in the `worktree-agent` cookbook
installs as `worktree-agent-setup`; two cookbooks whose scripts share a generic file name like
`setup` never collide once installed. `setup_action` and `cleanup_action` name one of those
sibling files by its bare name and are resolved to the installed name when the workflow is
created; both are optional, and a cookbook needs neither if its actions, like pr-reviewer's,
are only ever run on demand. `[workflow]` otherwise takes the same fields `create_workflow`
does, aside from `name`. `[schedule]` is optional; when present it is armed against the
workflow `install` just created under the cookbook's own name, so neither its `name` nor its
`workflow_id` are part of the manifest.

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

A cookbook has no identity of its own once installed; the workflow it created, named for the
cookbook itself, is the only record. `install` checks the live workflows for one already
holding that name and refuses if it finds one, so running `install` twice does not leave two
indistinguishable workflows behind. Archive or rename the existing one first if you mean to
install a second copy.

A cookbook cannot know your repository path or what your on-call rotation watches in advance,
so `--cwd` overrides the manifest's own working directory at install time, and the workflow's
context and cron expression are yours to edit afterward with `e` on the workflow in the panel,
or `weave rpc update-workflow` and `weave rpc update-schedule`.
