# Agents

An agent is just a managed claude or codex chat that you can step away from, reboot, search through, or automate. Some ideas:

- A worktree agent. Every agent you start with weave can be configured to get an isolated checkout, and takes its worktree with it when you delete it.
- A triage agent on a schedule. Try configuring a workflow with context explaining what your on-call rotation cares about, and a [schedule](schedules.md) that creates an agent every weekday morning with a prompt like "read the overnight alerts and write up anything that needs a human".
- An orchestrator. An agent whose prompt is to fan work out: it calls weave's [MCP](mcp.md) to `create_agent`for each subagent, and then communicates through Weave's mail.

## Creating your first agent

Press `c` in the sidebar. If you don't specify the agent, weave will use the default `default_harness` from your configuration. You can also create an agent via the CLI.

Agents can be thinking (generating or running a tool); waiting (blocked on a human confirmation or other interactive prompt); or idle (sitting with nothing to do).

Separately from state, an agent is either running or dormant. Dormant agents' panes are closed because the CLI has exited, but can be restored, by resuming the session.

## Adopting work that already exists

Adoption puts an agent on a branch somebody else made. Hover a worktree-enabled workflow's header, press `a`, and give it a pull request URL, a pull request number, or a branch name. It is the workflow's key rather than an agent's, since what you are adopting into is the workflow.

The workflow decides the repository, so a pull request has to be one opened against the repository its directory points at, and a pull request from a fork is refused: its branch is not on that repository's origin.

Where the agent runs depends on what is already there. When nothing has the branch checked out, the agent gets a worktree of its own on it, fetched from origin first. When some other checkout already holds it, made by hand or by another tool, the agent is launched into that directory as it stands and provisions nothing. When the branch is checked out in the workflow's own directory, adoption refuses, since that is the checkout you are working in.

The branch belongs to whoever made it. Weave never resets it, and deleting an adopted agent removes the worktree but leaves the branch alone. Adopting the same branch twice hands back the agent already on it rather than starting a second one, which git would refuse anyway.

The agent starts knowing what it walked into: the prompt it is given is prefixed with the branch, the pull request if there is one, and that the commits are not its own to rewrite. Leave the prompt blank and it is told nothing has been asked of it yet, and to say when it is ready and wait. A pull request is recorded on the agent as it starts, so the `o` key opens it straight away.

Agents can adopt too, through `adopt` over [MCP](mcp.md).

## Adopting a conversation that already exists

Weave can adopt agents as well as branches. Hover a workflow's header, press `A`, and give it the session id of a conversation somebody started outside the panel: what comes back is an ordinary agent, with a pane, a mailbox, and the whole conversation folded into the panel's search. Which CLI held it comes from where its log lives, and the working directory and branch out of the log itself.

A Claude conversation is forked rather than continued: the panel mints a session of its own carrying the history, so the conversation in the person's own terminal is left as it stands. Codex has no fork, so adopting one appends to the person's own rollout, and the agent is told so.

An adopted agent runs with no setup or teardown action, since the directory it works in is one somebody else made: nothing is provisioned for it, and deleting the agent leaves the checkout alone. Adoption refuses a directory another agent is working in, and a conversation one of the panel's own agents holds, since either would put two CLIs on one history. Adopting the same conversation twice hands back the agent already on it.

The agent is told what it walked into, and a blank prompt asks it to say where the conversation had got to and wait.

Agents can adopt a conversation too, through `adopt_agent` over [MCP](mcp.md).

## Metadata

Metadata is how an agent records what a person would want to open later. It is a set of key/value rows on the agent, written by the agent.

The panel shows those in the details under the list, after what the agent was launched with, and agents write them with `set_agent_metadata`, keyed on their own id from `WEAVE_AGENT_ID` environment variable.

## Archiving, restoring, deleting

Archiving an agent (the `z` key by default) stops the agent, kills its pane, and takes it off the panel, keeping the row, the metadata, and the transcript. Archiving is the ordinary way to finish with an agent.

The prompt that key opens offers archiving and "Archive and delete", which archives and then destroys the agent outright.

To see the full archive press the `Z` key. You can restore agents from the archive, or permanently delete them. Be careful: deletion destroys the row, the metadata, the flattened transcript, and its search index, and it fires the workflow's cleanup action, so a delete can tear down real directories on disk. Search will never find that conversation again. All actions can also be done in bulk via API, or via the panel, by selecting multiple agents with the `v` or `V` keys.

An archived workflow can be deleted the same way: hover its header row in the archive and press `d`. Its archived agents go with it through the same cleanup handling as deleting them directly, so a row whose cleanup is still running, or whose cleanup fails, is left behind and the workflow stays archived until nothing is left in it.

To clear the whole archive at once press `D`. It asks first, and then hands the work to a background job rather than holding the panel open, since deleting that many agents means running a cleanup action apiece. Rows whose cleanup is still running are left behind until it finishes, so press it again if anything is still there. A row whose cleanup action failed stays in the archive too, marked with a red `✗`, since the sweep could not destroy it: it is that mark, and the stage beside it, that say what went wrong. Any archived workflow the sweep leaves with nothing inside it is deleted too, rather than sitting there as an empty shell.
