---
title: MCP
description: How agents see and manage other agents through komrade's MCP server.
group: concepts
order: 8
---

Komrade serves its own API over MCP, the protocol agent CLIs use to call tools. That is what lets one agent coordinate others. An agent komrade started can ask what the other agents are doing, read their conversations, send them mail, and start new agents of its own. To komrade, a model calling a tool is one more client, the same as the panel or a script.

<figure class="placeholder">
  <figcaption>An orchestrator agent's pane, calling komrade tools to spawn two children and read their replies.</figcaption>
</figure>

## Who can reach it

Every agent komrade starts is already connected to the server, and nothing needs setting up. You can also add it to your own Claude or Codex session, which is how you drive komrade from a conversation outside the panel. The server only listens on your own machine and asks for no password. That is safe because nothing else can reach it, and it is also why exposing it anywhere else would be a real change.

For agents running Claude, komrade also makes sure its tools are always visible and switches off Claude's built-in subagent tool. An agent asked to spawn help then uses komrade, so the new agent appears in the panel where you can see it and step in.

## Two kinds of tool

The first kind reads komrade's database directly, with read-only SQL. It is there so an orchestrator can answer its own questions, like how many agents in a workflow are still working, without komrade needing a tool for every question. Queries are read-only, kept small, and cut off if they run long.

The second kind is the API itself: listing and fetching agents, creating them, sending mail, requesting reviews, and so on. These tools go through the same API as the panel, so they follow the same rules and checks. Anything that changes something, such as spawning or archiving an agent, goes through these, since the SQL tools cannot write.

When a model lists agents, it gets the same combined row the panel draws: the agent, its mark, whether mail is waiting, and its pull requests and review state. What a model sees and what you see stay in step.

The [API reference](../../api/) lists every tool and what it takes.

## Different roles, different tools

Every tool in an agent's list sits in its context from its first turn, and a long list costs attention as well as tokens. So komrade only offers each agent the tools that fit its role.

An implementer, the agent writing a change, gets what it needs to do the work, get it reviewed and answer the review. It can read workflows but not change them, and it cannot administer the panel. A reviewer gets what it needs to read work and write a review. An orchestrator, which hands work out and writes no code, gets everything except the review-writing tools. Each role ends up with well under half of the full set.

A caller komrade cannot place, such as you with the server added to your own session, is offered everything. None of this affects the `komrade rpc` commands.

## Knowing who called

Every agent carries its own id, and komrade attaches it to every call the agent makes. That is how a call is credited to the agent that made it, how the server knows which role to serve, and how a family of agents stays inside its own limits. Calls from the panel carry no id, because the panel is you.

The SQL tools connect with a separate read-only login that each engine is given when it is set up. It can only see that engine's own rows, so no query can reach another engine's data, however it is written.
