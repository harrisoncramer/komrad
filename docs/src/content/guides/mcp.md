---
title: MCP
description: Let agents see and manage other agents through weave's MCP server.
group: concepts
order: 8
---

Weave serves its API over MCP, which is what makes orchestration possible. An agent weave spawned can ask what the other agents are doing and spawn more of them. The panel and every agent are clients of the same service.

The endpoint is `http://127.0.0.1:8787/mcp`, or wherever `WEAVE_ADDR` points. It has no authentication. It only listens on loopback, and its tools can spawn and delete agents, so binding the service to any other address is a real change in who can reach it.

Do not call the HTTP API with hand-written curl. That skips the checks the tools carry and the headers that say who made the call. From a shell, use `weave rpc`, which is covered in the [CLI reference](../../cli/rpc/).

## Adding it to a client

Agents that weave launches already have it. Weave also turns on `alwaysLoad` for them, so Claude does not hide the weave tools behind its tool search. And it denies Claude's general-purpose subagent tool, so an agent asked to spawn something uses weave instead.

For your own Claude session:

```sh
claude mcp add --transport http weave http://127.0.0.1:8787/mcp
```

For a client that takes JSON:

```json
{
  "mcpServers": {
    "weave": {
      "type": "http",
      "url": "http://127.0.0.1:8787/mcp"
    }
  }
}
```

For Codex, which takes TOML:

```toml
[mcp_servers.weave]
url = "http://127.0.0.1:8787/mcp"
```

Name the server `weave`. Nothing enforces it, but the briefing every spawned agent reads calls it that.

## Two kinds of tool

The first kind is read-only SQL over weave's Postgres, through `list_tables`, `describe_table` and `query`. An orchestrator can use these to answer its own questions without weave needing a dedicated tool for each one. `query` takes a single `SELECT` or `WITH`, refuses writes, returns at most 1000 rows, cuts cells at 2000 characters, and gives up after 15 seconds.

The second kind is the service's own API as tools. Each call goes back over loopback to the API, so it takes the same path and the same checks as any other client. Spawning, renaming and removing agents can only be done this way, because the SQL tools cannot write.

Agents use the API tools to change things, and SQL when the question is a count or a join. Examples are how many agents in each workflow are still thinking, or which of my children recorded a pull request.

A tool that lists or fetches something answers rows of that one kind. The exception is `list_agents` and `get_agent`, which answer the agent with what the panel draws beside it: its mark, whether mail is waiting, and its pull requests. They build that the same way the panel does, so a model sees what you see in one call.

The [API reference](../../api/) lists every tool.

## Who gets which tools

Every tool declares which agent roles it is offered to, and the proto linter refuses a tool that leaves this out. Each call names its caller in the `X-Weave-Agent-Id` header, and the server serves that caller's role.

A reviewer gets the tools for reading work and writing a review. An implementer gets the tools for getting its own work read and answering what comes back. It does not get the review-writing tools or the panel's administration, such as workflows, schedules, rebases, worktrees and forges. An orchestrator gets everything except the review-writing tools and `request_review`, because it writes no code and reviews nothing.

The point is context cost. Every tool description sits in an agent's context from its first turn, and an implementer or reviewer is served well under half the full set. A caller weave cannot place, such as you with a server added by hand, is served every tool. None of this applies to `weave rpc`.

## The read-only login

The `query` tool connects as the engine's read-only login, which `weave engine ensure` creates. It can only read, and Postgres only shows it the engine's own rows, so no query can reach another engine's data. The service will not start without it.

## Who a call is from

An agent's id is in `WEAVE_AGENT_ID`, and its conversation id is in `WEAVE_SESSION_ID`. Weave sets both before starting the CLI and adds them as headers on every call the agent makes, so each call is credited to the agent that made it. The panel sends neither, because it is you.
