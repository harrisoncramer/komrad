# Agents

An agent is just a CLI process, running in a tmux window and tracked by weave. Think of it as a managed claude or codex chat that you can step away from, reboot, search through, or automate. Some ideas:

- A worktree agent. One workflow per repository, with a setup action that creates a git worktree and prints it, and a cleanup action that removes it. Every agent you start there gets an isolated checkout, records its branch and pull request as metadata, and takes its worktree with it when you delete it.
- A triage agent on a schedule. A workflow pointed at your repository with context explaining what your on-call rotation cares about, and a [schedule](schedules.md) that creates an agent every weekday morning with a prompt like "read the overnight alerts and write up anything that needs a human".
- An orchestrator. An agent whose prompt is to fan work out: it calls `create_agent` for each piece, waits, reads their transcripts, and writes up what they found. It gets the panel's MCP tools like any other agent, so the only thing making it an orchestrator is its prompt. See [MCP](mcp.md).

## Creating your first agent

Press `n` in the panel from inside a workflow. If you don't specify the agent to use, weave will use the default `default_harness` from your configuration file. You can also create an agent via the CLI.

Agents can be thinking (generating or running a tool); waiting (blocked on a human confirmation or other interactive prompt); or idle (sitting at its input prompt with nothing to do).

Separately from state, an agent is either running or dormant. Dormant agents' panes are closed because the CLI has exited, but can be restored with the `enter` key by default. If the workflow has a setup action, it runs again first, so a resume can rebuild the worktree the agent needs.

## Metadata

Metadata is how an agent records what a person would want to open later. It is a set of key/value rows on the agent, written by the agent itself as it works, and nothing else writes them.

The panel shows those beside the agent, and agents write them with `set_agent_metadata`, keyed on their own id from `WEAVE_AGENT_ID` environment variable. Tell your agent to write here to store data you'll need in an action: PR links, for instance, or slack discussion data.

## Archiving, restoring, deleting

Archiving an agent (the `z` key by default) stops the agent, kills its pane, and takes it off the panel, keeping the row, the metadata, and the transcript. Archiving is the ordinary way to finish with an agent.

The prompt that key opens offers archiving and "Archive and delete", which archives and then destroys the agent outright, so something you have no intention of coming back to takes one answer rather than a second trip through the archive screen. It opens on plain archiving, so the destructive choice is a deliberate move down.

To see the full archive press the `Z` key. You can restore agents from the archive, or permanently delete them. Be careful: deletion destroys the row, the metadata, the flattened transcript, and its search index, and it fires the workflow's cleanup action, so a delete can tear down real directories on disk. Search will never find that conversation again. All actions can also be done in bulk via API, or via the panel, by selecting multiple agents.

## What an agent knows about itself

Every agent is launched with two environment variables. The `WEAVE_AGENT_ID` value is weave's own id for that agent. The `WEAVE_SESSION_ID` is the id of the conversation it is in.

By default, agnets use these to scope certain actions, such as searching. Inside a pane, they are just variables:
