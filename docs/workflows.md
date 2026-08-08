# Workflows

Workflows are the blueprints used to create one first.

Every agent belongs to exactly one workflow, which is why weave makes you create one first.

## What a workflow holds

Since agents are just CLI sessions, workflows are buckets of defualt settings for those agents. For instance, a workflow might indicate which model to use (like opus or haiku), the working directory, or specific actions to run on startup or deletion, relevant for worktree developmemt.

Ultimately, each workflow setting for an agent can be overridden at the moment when an agent is spawned; this approach provides flexibility, for instance when you'd like to use a particular workflow, but with a different model or different directory.

Workflows also hold shared context. Represented as free text, this is appended to the system prompt of every agent created inside the workflow.

Which CLI the agents run is known as the "harness." Currently, `claude` and `codex` are supported. Like all settings, agents already spawned keep the harness they started on, so changing this only affects new ones.

## The context

Weave assembles the appended system prompt in three layers, general to specific: the standing briefing about weave itself (the MCP, how to send mail, etc), your global context file, shared across all agents, and then the workflow's context. The workflow comes last, so it reads as the final word on anything the layers above it also mention. The global file is configured under `[context]` in your [config](configuration.md).

This context could include general conventions, such as:

- Repo conventions. Where things live, which generator owns which directory, how tests are run.
- Naming rules. Branch names, commit style, what an agent should call the PR it opens.
- Boundaries. What agents in this group are allowed to touch, and what they must leave alone.
- Where to report. Which metadata keys to record on themselves so you can find their work later.

The context is baked into the agent's system prompt at launch, so editing a workflow reaches only the agents spawned after the edit. Agents already running keep what they started with.

## Setup and cleanup

The setup action is the part worth being precise about, because a launch waits on it.

It runs before the tmux window is opened, since where that window opens is one of the things setup gets to decide. It blocks, with a ten minute ceiling. If it exits non-zero, the launch is abandoned: no window, no row, no agent, and the error comes back to you with whatever the script wrote on stderr. Nothing is left behind to clean up, so the cleanup action does not fire either.

If it exits zero, whatever it printed on stdout, trimmed, is the directory the agent runs in. Print nothing and the agent runs in the directory the workflow or the form chose. This is why progress messages go on stderr: stdout is a directory, and a script that chats there is refused rather than taken at its word. If it prints something that is not a usable directory, the launch is abandoned and the cleanup action does fire, since setup may already have built something.

The script gets `WEAVE_TRIGGER=setup` on a create and `WEAVE_TRIGGER=resume` when a dormant agent is coming back. Same script, both times. Resume matters because archiving an agent leaves whatever setup built alone, and a worktree can easily be gone by the time you bring the agent back. What your script does about a workspace that is still there is its own call.

Cleanup fires when an agent is deleted for good, not when it is archived. Nothing waits on it and nothing is downstream of it, so a failure is logged and the delete goes through anyway. An agent has to be deletable whatever state your actions directory is in.

One action each rather than a list, because an action is a script and a script already sequences its own steps, with error handling a list could not express.

## A worktree per agent

The common shape: every agent in the workflow gets its own git worktree, works there, and the worktree goes away when the agent is deleted. This is the default setup type, rather than something you turn on: a new workflow opens with it already chosen, and the setup and cleanup actions are the other setup type, `Custom`, not a separate toggle alongside it. The two are exclusive, since the worktree fields and the action pickers fill the same role, and the form shows one set or the other depending which you have picked.

In the panel, `w`, with the directory set to the repo the worktrees are added off:

```
New workflow name    myrepo features
Context              Work only inside the worktree you were started in.
                     Branch is already made for you, do not create another.
                     Run `mise run check` before you say you are done.
                     Record the PR url on yourself with set_agent_metadata under `pr`.
Default agent        claude
Default directory    ~/myrepo
Default flags        --model opus
Setup type           Worktree: a git worktree per agent
Worktree base branch     (blank for the repo's default branch)
Worktree copy paths      .env
Worktree symlink paths   node_modules
```

Submitting points the workflow's setup and cleanup actions at two built-in scripts, `_worktree_setup` and `_worktree_cleanup`, shipped with weave the same way `_edit_action` is rather than written out per workflow. Nothing else about the workflow's lifecycle changes: setup still blocks a launch and its stdout still names the directory the agent opens in, cleanup still runs best-effort on delete. The base branch and the two path lists live on the workflow's own row and are what those two scripts read at the moment they run; they are not settings on the actions themselves, since one pair of built-ins serves every worktree workflow.

Setup fetches the base branch when the repo has a remote, adds the worktree at a path under your `[worktrees].dir` (`~/.local/state/weave/worktrees` unless you have configured otherwise) named after the agent's id, and applies the copy and symlink paths, one repo-relative path per line: a copy path is a file such as `.env` written into the worktree, and a symlink path is a directory such as `node_modules` linked back to the repo instead of duplicated. A path that is not in the repo is skipped with a warning on stderr rather than failing the whole setup. All of that happens only for a worktree the run itself made, so on `WEAVE_TRIGGER=resume` a worktree that is still there is left exactly as the agent left it. Cleanup removes the worktree and its branch, both best-effort, since a workflow's cleanup action must not block a delete.

Switching the setup type to `Custom` clears `Setup action` and `Cleanup action` if they still name the two built-ins, leaving them alone if you had already pointed the workflow at something else. The base branch and the two path lists are not cleared either way, so switching back to `Worktree` later hands them back rather than starting over blank.

Press `n` with the cursor anywhere in that workflow to start an agent in it. It gets its own worktree, its own branch, and the context above on top of its prompt. Any agent whose directory looks like a git checkout, however it got one, shows its current branch beside its name in the tree.

### By hand, if the built-in scripts don't cover it

The built-in setup and cleanup scripts are sugar over the setup and cleanup action seam, not a new lifecycle, so writing your own still works under `Custom`, and is the way to reach anything the built-ins' copy and symlink lists do not cover, such as a hook or a database migration run once the worktree exists.

Write `~/.config/weave/actions/make-worktree`, executable:

```bash
#!/usr/bin/env bash
set -euo pipefail

repo=~/myrepo
tree="$HOME/trees/$WEAVE_AGENT_ID"

if [ ! -d "$tree" ]; then
  echo "creating a worktree at $tree" >&2
  git -C "$repo" worktree add -b "agent/$WEAVE_AGENT_ID" "$tree" main >&2
fi

echo "$tree"
```

Note the two rules. Everything chatty goes to stderr with `>&2`, including git's own output. The only thing on stdout is the path, and that is what the agent's pane opens in. The existence check is what makes it safe on `WEAVE_TRIGGER=resume`, where the tree is usually still there.

Then `~/.config/weave/actions/drop-worktree`:

```bash
#!/usr/bin/env bash
set -euo pipefail

repo=~/myrepo
git -C "$repo" worktree remove --force "$HOME/trees/$WEAVE_AGENT_ID" || true
git -C "$repo" branch -D "agent/$WEAVE_AGENT_ID" || true
```

Set both as the workflow's setup and cleanup actions, with the setup type set to `Custom`, the same way any hand-written pair is set.

## From the panel

`w` creates a workflow from anywhere in the agents list. `e` on a workflow header opens the same form prefilled, so you can change any of it. `r` renames. `z` archives.

Setup type picks between the worktree fields and a pair of action pickers. The pickers list what your actions directory actually holds; you cannot type the name of a script that does not exist, and if a workflow names one you later renamed, the form shows it marked `(missing)` rather than silently dropping it. Switching the setup type swaps which set of fields is on screen, rather than showing both or graying one out.

Long context is painful in a text box, so `ctrl+e` in that field opens `$EDITOR`. The two worktree path fields take the same kind of text, one path per line rather than one field per path.

## From MCP

Same fields, over the [MCP endpoint](mcp.md), which is how an orchestrating agent sets up a group before spawning into it:

```
create_workflow {
  "name": "myrepo features",
  "context": "Work only inside the worktree you were started in.",
  "cwd": "~/myrepo",
  "flags": "--model opus",
  "harness": "claude",
  "worktree": {
    "enabled": true,
    "copyPaths": [".env"],
    "symlinkPaths": ["node_modules"]
  }
}
```

`setupAction` and `cleanupAction` are left out here since `worktree.enabled` writes and names both itself; sending one alongside it is refused. Leave `worktree` out entirely, or send it with `enabled` false, to name your own actions as before.

`list_workflows` pages through them, most recently updated first, and takes an `archived` flag that lists the archived ones and only those. `get_workflow` fetches one by id. Call `list_actions` first if you are choosing a setup action, since the name has to match a real file.

`update_workflow` replaces every field, so an omitted one reads as cleared rather than unchanged. Read the workflow, change the field you mean, send the whole thing back.

## Archiving and restoring

Archiving a workflow hides it and archives every agent inside it, in one transaction. The agents go with it because there is nowhere else for an agent to live. Nothing is destroyed: the rows stay, the transcripts stay, and `search_transcripts` still finds the conversations.

It is refused while any schedule still launches into the workflow, since a schedule reads its directory and flags off the workflow when it fires and would have nowhere to run. Turn those schedules off first, deliberately.

Archiving does not run the cleanup action, so whatever setup built is still on disk.

`Z` opens the archive, which is the same tree: workflow headers with their archived agents under them. `r` on an archived header restores the workflow along with every agent archived inside it. They come back dormant rather than running, so a folder of twelve does not start twelve CLIs. Open one from the tree, or call `resume_agent`, to put its conversation back in a pane. That resume is what fires the setup action again and rebuilds the workspace.

A restore is all or nothing. It is refused if the returning agents would not fit under your configured agent ceiling, or if a live agent has taken one of their names, and the error names what is in the way so you can clear it and press `r` again.

There is no hard delete for a workflow. Agents inside one can be destroyed individually from the archive with `d`, which is the only thing in the panel nothing undoes.

## Setting up your first one

Open the panel with `prefix + g`. Press `w`, give it a name, leave everything else blank, and submit. Press `n` and give an agent a prompt. That is the whole minimum: a workflow with nothing but a name is just a folder.

Then add to it as you learn what the group needs. A default directory once you are tired of typing it. Context once you have told three agents the same thing. A setup action once you want them out of each other's way.
