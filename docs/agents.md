# Agents

An agent is just a managed claude or codex chat that you can step away from, reboot, search through, or automate. Some ideas:

- A worktree agent. Every agent you start with weave can be configured to get an isolated checkout, and takes its worktree with it when you delete it.
- A triage agent on a schedule. Try configuring a workflow with context explaining what your on-call rotation cares about, and a [schedule](schedules.md) that creates an agent every weekday morning with a prompt like "read the overnight alerts and write up anything that needs a human".
- An orchestrator. An agent whose prompt is to fan work out: it calls weave's [MCP](mcp.md) to `create_agent`for each subagent, and then communicates through Weave's mail.

## Creating your first agent

Press `c` in the sidebar. If you don't specify the agent, weave will use the default `default_harness` from your configuration. You can also create an agent via the CLI.

Agents can be thinking (generating or running a tool); waiting (blocked on a human confirmation or other interactive prompt); idle (sitting with nothing to do); or uncertain, drawn as a grey `?`, which is a pane weave captured but could not read. An uncertain agent is not a finished one: weave withholds its mail, leaves it out of the auto-archive sweep, and holds a schedule back for it, because the screen it could not read may be a question waiting on you.

An agent that finishes a turn while you are looking somewhere else carries a `●` at the end of its row until you have looked at it. The turn end comes from the agent's own CLI saying so, through the hook weave installs, and from weave watching the pane settle, so a CLI that drops its end-of-turn event cannot swallow the mark. An agent that is still working carries nothing either way. An agent that goes again after finishing, because mail woke it or somebody typed at it, drops the mark for as long as it works and takes a new one when that turn ends. Hovering a running agent puts its own pane in the stage, so hovering is how one is read, and the mark clears once the cursor has been sitting on it for a second rather than the moment it arrives. Press `n` to jump to the next agent carrying something unread and `N` to go back, and `u` to put a mark back, or take one off, by hand. A working agent has no mark to move, so `u` on one does nothing until its turn ends.

Separately from state, an agent is either running or dormant. Dormant agents' panes are closed because the CLI has exited, but can be restored, by resuming the session.

## Adopting work that already exists

Adoption puts an agent on a branch somebody else made. Hover a worktree-enabled workflow's header, press `a`, and give it a pull request URL, a pull request number, or a branch name. It is the workflow's key rather than an agent's, since what you are adopting into is the workflow.

The workflow decides the repository, so a pull request has to be one opened against the repository its directory points at, and a pull request from a fork is refused: its branch is not on that repository's origin.

Where the agent runs depends on what is already there. When nothing has the branch checked out, the agent gets a worktree of its own on it, fetched from origin first. When some other checkout already holds it, made by hand or by another tool, the agent is launched into that directory as it stands and provisions nothing. When the branch is checked out in the workflow's own directory, adoption refuses, since that is the checkout you are working in.

The branch belongs to whoever made it. Weave never resets it, and deleting an adopted agent removes the worktree but leaves the branch alone. Adopting the same branch twice hands back the agent already on it rather than starting a second one, which git would refuse anyway.

The agent starts knowing what it walked into: the prompt it is given is prefixed with the branch, the pull request if there is one, and that the commits are not its own to rewrite. Leave the prompt blank and it is told nothing has been asked of it yet, and to say when it is ready and wait. A pull request is recorded on the agent as it starts, so the `o` key opens it straight away.

Agents can adopt too, through `adopt` over [MCP](mcp.md).

## Children

A child agent is attached to another agent rather than standing on its own. It runs in that agent's own directory, provisions nothing of its own, may only mail inside its family, and goes when the parent goes. The panel draws it indented under the row it belongs to.

A child is the default shape when an agent spawns another, and what decides it is whether the new agent reads the parent's tree or writes in it. Reading is a second pair of eyes on work already in progress: an author that wants its change read, or a test runner that reports what failed. Writing work of its own needs a checkout of its own, so that agent stands on its own instead. Doing the reading case with an ordinary agent means passing the parent's directory and turning its provisioner off by hand, and getting either wrong drops the agent into a fresh checkout where there is nothing to read. A child takes both from the parent, so there is nothing to get wrong.

Press `C` on the hovered agent to attach one. The form asks for the prompt, a title, the harness and the flags, and nothing about where it runs, since none of that is yours to choose here. Agents attach children too, through `create_agent` over [MCP](mcp.md) with `parentAgentId` set to their own id.

What a child cannot do is the point of it. It has no directory of its own, so it is a guest in the parent's checkout: it is told to read and run but not to commit, push, stash, or switch branches. It cannot have children of its own. And it can only reach its parent and that parent's other children by [mail](mail.md).

Archiving or deleting the parent takes the children with it, and a child named on its own goes on its own, which is how you finish with a reviewer while the agent it was reading for keeps working. Restoring is the one direction that holds the family together: a child comes back only once its parent is live again, since there would otherwise be no row for it to be drawn under.

Because they all edit the one checkout, one agent may hold only a few children at once. `max_children` in your configuration is that ceiling, and a call past it is refused rather than queued.

## Metadata

Metadata is how an agent records what a person would want to open later. It is a set of key/value rows on the agent, written by the agent.

The panel shows those in the details under the list, after what the agent was launched with, and agents write them with `set_agent_metadata`, keyed on their own id from `WEAVE_AGENT_ID` environment variable.

## Archiving, restoring, deleting

Archiving an agent (the `z` key by default) stops the agent, kills its pane, and takes it off the panel, keeping the row, the metadata, and the transcript. Archiving is the ordinary way to finish with an agent.

The prompt that key opens offers archiving and "Archive and delete", which archives and then destroys the agent outright.

To see the full archive press the `Z` key. You can restore agents from the archive, or permanently delete them. Be careful: deletion destroys the row, the metadata, the flattened transcript, and its search index, and it removes the agent's worktree, so a delete can tear down real directories on disk. Search will never find that conversation again. All of these can also be done in bulk via API, or via the panel, by selecting multiple agents with the `v` or `V` keys.

An archived workflow can be deleted the same way: hover its header row in the archive and press `d`. Its archived agents go with it through the same deprovision handling as deleting them directly, so a row whose deprovision is still running, or whose deprovision fails, is left behind and the workflow stays archived until nothing is left in it.

To clear the whole archive at once press `D`. It asks first, and then hands the work to a background job rather than holding the panel open, since deleting that many agents means removing a worktree apiece. Rows whose worktree is still being removed are left behind until it finishes, so press it again if anything is still there. A row whose removal failed stays in the archive too, marked with a red `✗`, since the sweep could not destroy it: it is that mark, and the stage beside it, that say what went wrong. Any archived workflow the sweep leaves with nothing inside it is deleted too, rather than sitting there as an empty shell.
