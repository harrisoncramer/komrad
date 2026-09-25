---
title: MCP
description: How agents see and manage other agents through komrad's MCP server.
group: start
order: 7
---

In the [mail](../mail/) guide, the first agent spawned the second one itself. It did that through MCP. Komrad serves its own API over MCP, the protocol agent CLIs use to call tools, so an agent can ask what the other agents are doing, read their conversations, send them mail, and start agents of its own. To komrad, a model calling a tool is one more client, the same as the panel.

<figure class="placeholder">
  <figcaption>An orchestrator agent's pane, calling komrad tools to spawn two children and read their replies.</figcaption>
</figure>

## Who can reach it

Every agent komrad starts is already connected. You can also add the server to your own Claude or Codex session to drive komrad from a conversation. The server listens only on your own machine and asks for no password.

For Claude agents, komrad also switches off Claude's built-in subagent tool. An agent asked to spawn help then uses komrad, so the new agent appears in the panel where you can see it.

## Two kinds of tool

The first kind reads komrad's database with read-only SQL, so an orchestrator can answer its own questions, like how many agents in a workflow are still working. The second kind is the API itself: listing agents, creating them, sending mail, requesting reviews. Anything that changes something goes through the API, with the same rules as the panel. When a model lists agents it gets the same row the panel draws.

The [API reference](../../api/) lists every tool.

## Different roles, different tools

Every tool sits in an agent's context from its first turn, so a long list costs attention. Komrad offers each agent only the tools that fit its role. An implementer gets what it needs to do the work and answer a review. A reviewer gets what it needs to read work and write one. An orchestrator gets everything except the review-writing tools. You, with the server in your own session, get everything.

## Knowing who called

Every agent carries its own id, and komrad attaches it to every call. That is how a call is credited to the agent that made it and how the server knows which role to serve. Calls from the panel carry no id, because the panel is you.

Your agents can now start and talk to each other. Next, connect komrad to your [forge](../forges/) so they can open pull requests.
