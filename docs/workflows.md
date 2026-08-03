# Workflows

A workflow is a named bucket of related agents, and the template they are created from. It runs nothing on its own. Everything it holds is either a default that gets prefilled when you create an [agent](agents.md) inside it, or something weave reads at the moment that agent starts, resumes, or is deleted.

Every agent belongs to exactly one workflow. There is no way to have a loose agent, which is why the panel makes you create a workflow before your first agent.

TODO TODO TODO: Write workflow file.

## What a workflow holds

A name. It is a display label, not a key, so you can rename it whenever. Agent names are unique within a workflow, so two workflows can each have an agent called `auth`.

Context. Free text, appended to the system prompt of every agent created inside the workflow. This is the interesting field, and there is a section on it below.

A working directory and flags. These are the defaults prefilled into the new-agent form, and the ones a [schedule](schedules.md) uses when it fires into the workflow with nobody around to fill a form in. An agent that names its own directory or flags wins over the workflow's. The directory has to be absolute or start with `~`.

A harness. Which CLI the agents here run, `claude` or `codex`. Blank means they take whatever you configured as your default. Agents already spawned keep the harness they started on, so changing this only affects new ones.

A setup action and a cleanup action. Both name a file in your actions directory. Setup runs before an agent in this workflow starts, and again before a dormant one is resumed. Cleanup runs once one of them is deleted. See [actions](actions.md) for how those scripts are found and what they are handed.

## Why context lives here

You could paste the same paragraph into every prompt. The reason not to is that the prompt is the task and the context is the ground rules, and they change on different schedules. The task is different every time. The ground rules are the same for the twentieth agent as for the first, and when they turn out to be wrong you want to fix them in one place.

Weave assembles the appended system prompt in three layers, general to specific: the standing briefing about the panel itself, then your global context file, then the workflow's context. The workflow comes last, so it reads as the final word on anything the layers above it also mention. The global file is configured under `[context]` in your [config](configuration.md).

What belongs in it is anything true of every agent in the group and nothing else:

- Repo conventions. Where things live, which generator owns which directory, how tests are run.
- Naming rules. Branch names, commit style, what an agent should call the PR it opens.
- Boundaries. What agents in this group are allowed to touch, and what they must leave alone.
- Where to report. Which metadata keys to record on themselves so you can find their work later.

What does not belong: anything about one specific task, and anything that changes per agent. That is what the prompt is for.

The context is baked into the agent's system prompt at launch, so editing a workflow reaches only the agents spawned after the edit. Agents already running keep what they started with. The two actions are different: they are looked up when they fire, so changing the cleanup action reaches agents that are already in the pane.

## Setup and cleanup

The setup action is the part worth being precise about, because a launch waits on it.

It runs before the tmux window is opened, since where that window opens is one of the things setup gets to decide. It blocks, with a ten minute ceiling. If it exits non-zero, the launch is abandoned: no window, no row, no agent, and the error comes back to you with whatever the script wrote on stderr. Nothing is left behind to clean up, so the cleanup action does not fire either.

If it exits zero, whatever it printed on stdout, trimmed, is the directory the agent runs in. Print nothing and the agent runs in the directory the workflow or the form chose. This is why progress messages go on stderr: stdout is a directory, and a script that chats there is refused rather than taken at its word. If it prints something that is not a usable directory, the launch is abandoned and the cleanup action does fire, since setup may already have built something.

The script gets `WEAVE_TRIGGER=setup` on a create and `WEAVE_TRIGGER=resume` when a dormant agent is coming back. Same script, both times. Resume matters because archiving an agent leaves whatever setup built alone, and a worktree can easily be gone by the time you bring the agent back. What your script does about a workspace that is still there is its own call.

Cleanup fires when an agent is deleted for good, not when it is archived. Nothing waits on it and nothing is downstream of it, so a failure is logged and the delete goes through anyway. An agent has to be deletable whatever state your actions directory is in.

One action each rather than a list, because an action is a script and a script already sequences its own steps, with error handling a list could not express.

## A worked example: a worktree per agent

The common shape. Every agent in the workflow gets its own git worktree, works there, and the worktree goes away when the agent is deleted.

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

Now create the workflow. In the panel, `w`:

```
New workflow name    myrepo features
Context              Work only inside the worktree you were started in.
                     Branch is already made for you, do not create another.
                     Run `mise run check` before you say you are done.
                     Record the PR url on yourself with set_agent_metadata under `pr`.
Default agent        claude
Default directory    ~/myrepo
Default flags        --model opus
Setup action         make-worktree
Cleanup action       drop-worktree
```

The default directory is a fallback here, since setup overrides it on stdout every time. It is still worth setting, because it is what a form shows you and what a schedule falls back to if the action ever prints nothing.

Press `n` with the cursor anywhere in that workflow to start an agent in it. It gets its own worktree, its own branch, and the context above on top of its prompt.

## From the panel

`w` creates a workflow from anywhere in the agents list. `e` on a workflow header opens the same form prefilled, so you can change any of it. `r` renames. `z` archives.

The two action fields are pickers, not text inputs, listing what your actions directory actually holds. You cannot type the name of a script that does not exist. If a workflow names a script you later renamed, the form shows it marked `(missing)` rather than silently dropping it.

Long context is painful in a text box, so `ctrl+e` in that field opens `$EDITOR`.

## From MCP

Same fields, over the [MCP endpoint](mcp.md), which is how an orchestrating agent sets up a group before spawning into it:

```
create_workflow {
  "name": "myrepo features",
  "context": "Work only inside the worktree you were started in.",
  "cwd": "~/myrepo",
  "flags": "--model opus",
  "harness": "claude",
  "setupAction": "make-worktree",
  "cleanupAction": "drop-worktree"
}
```

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
