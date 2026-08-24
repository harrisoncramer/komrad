# Agents

An agent is just a managed claude or codex chat that you can step away from, reboot, search through, or automate. Some ideas:

- A worktree agent. Every agent you start with weave can be configured to get an isolated checkout, and takes its worktree with it when you delete it.
- A triage agent on a schedule. Try configuring a workflow with context explaining what your on-call rotation cares about, and a [schedule](schedules.md) that creates an agent every weekday morning with a prompt like "read the overnight alerts and write up anything that needs a human".
- An orchestrator. An agent whose prompt is to fan work out: it calls weave's [MCP](mcp.md) to `create_agent`for each subagent, and then communicates through Weave's mail.

## Creating your first agent

Press `n` in the sidebar. If you don't specify the agent, weave will use the default `default_harness` from your configuration. You can also create an agent via the CLI.

Agents can be thinking (generating or running a tool); waiting (blocked on a human confirmation or other interactive prompt); or idle (sitting with nothing to do).

Separately from state, an agent is either running or dormant. Dormant agents' panes are closed because the CLI has exited, but can be restored, by resuming the session.

## Metadata

Metadata is how an agent records what a person would want to open later. It is a set of key/value rows on the agent, written by the agent.

The panel shows those in the details under the list, after what the agent was launched with, and agents write them with `set_agent_metadata`, keyed on their own id from `WEAVE_AGENT_ID` environment variable.

## Archiving, restoring, deleting

Archiving an agent (the `z` key by default) stops the agent, kills its pane, and takes it off the panel, keeping the row, the metadata, and the transcript. Archiving is the ordinary way to finish with an agent.

The prompt that key opens offers archiving and "Archive and delete", which archives and then destroys the agent outright.

To see the full archive press the `Z` key. You can restore agents from the archive, or permanently delete them. Be careful: deletion destroys the row, the metadata, the flattened transcript, and its search index, and it fires the workflow's cleanup action, so a delete can tear down real directories on disk. Search will never find that conversation again. All actions can also be done in bulk via API, or via the panel, by selecting multiple agents with the `v` or `V` keys.
