# MCP

Weave serves itself over MCP, which is what makes an orchestrator possible: an agent weave spawned can ask the panel what the other agents are doing, and spawn more of them. The panel and an agent are both just clients of the same service.

The endpoint is `http://127.0.0.1:8787/mcp`, or wherever `WEAVE_ADDR` points. There is no auth on it. It listens on loopback, and the writes let a caller spawn and delete agents, so binding the service to a non-loopback address is a real change in exposure.

Do not hand-roll curl against the HTTP API. It skips the validation the tools carry, skips the headers that attribute the call.  Use `weave rpc` from a shell; see [commands](commands.md).

## Registering it

Agents weave launches already have it, on every launch and every resume. Their config carries `alwaysLoad`, which exempts the server from Claude's tool search: without it every weave tool is deferred behind a lookup step while the CLI's own subagent tool is already in context, and an agent told to spawn something reaches for the one it can see. The same launch denies `Agent(general-purpose)` and `Agent(claude)` so it cannot, while leaving the read-only search agents alone. For your own session:

    claude mcp add --transport http weave http://127.0.0.1:8787/mcp

For a client taking JSON:

    {
      "mcpServers": {
        "weave": {
          "type": "http",
          "url": "http://127.0.0.1:8787/mcp"
        }
      }
    }

For Codex, which takes TOML:

    [mcp_servers.weave]
    url = "http://127.0.0.1:8787/mcp"

Name it `weave`. Nothing enforces it, but the briefing every spawned agent gets calls it that.

## Two halves

One half is read-only SQL over the panel's Postgres: `list_tables`, `describe_table`, and `query`. That exists so an orchestrator can ask its own questions without weave shipping an RPC for each one. `query` takes a single `SELECT` or `WITH`, rejects writes and DDL, caps at 1000 rows, truncates a cell at 2000 characters, and gives up after 15 seconds.

The other half is the panel's own RPCs as tools. Each call goes back out over loopback to the API rather than straight into the service, so a tool call takes the same path a real client does, validation included. Spawning, renaming, and removing an agent are only possible here, since the SQL half cannot write.

Agents use the tools to change things, as they allow writes, and the general SQL engine when the question is an aggregate or a join: how many agents in each workflow are still thinking, which of my children recorded a pull request URL, and so forth.

A tool that lists or fetches agents, workflows, schedules, marks, mail or pull requests answers rows of that one kind and joins nothing to them. The one exception is what `list_agents` and `get_agent` answer, which is the agent with what the panel draws beside it: its mark, whether mail is waiting for it, and the pull requests opened from its branch with the current one's standing. That row is put together the same way the panel puts it together, so a model gets in one call what the panel shows, and a write's response carries the bare agent it wrote.

## Who is served what

Every tool declares which agent roles it is offered to, in the same `surfaces` annotation that says whether it reaches the CLI at all. Naming them is required rather than defaulted, and `buf lint` refuses a tool that leaves them out, so a tool added later is a decision about who carries it rather than a silent addition to everybody. The caller names itself on every request through `X-Weave-Agent-Id`, so the server reads its role and hands it that role's tools.

A reviewer gets the tools for reading work and writing a review, and not `request_review`. An implementer gets the tools for getting its own work read and answering what comes back, and not the review-writing half or the panel's administration: workflows, schedules, rebases, worktrees, forges, and the agent lifecycle. An orchestrator gets everything but the review-writing half and `request_review`, since it writes no code and reviews nothing.

The point is what a tool list costs. The whole surface is 62 tools and around 13,000 tokens, standing in every agent's context from its first turn; an implementer is served 22 of them and a reviewer 24, which is around 6,000. A caller weave cannot place, such as a person who registered the server by hand with `claude mcp add`, is served every tool, and none of this reaches `weave rpc`, where a person running a command holds no role.

## The read-only role

The `query` tool connects as the engine's read-only login, which `weave engine ensure` creates beside the one the engine writes with. It can only read, and Postgres shows it the engine's own rows and no other's, so a query cannot reach another engine's data however it is written.

Without a working read-only login the service refuses to start.

## Who a call is from

An agent's id is in `WEAVE_AGENT_ID` and its conversation id in `WEAVE_SESSION_ID`. Weave exports both ahead of the CLI and turns them into headers on every call the agent makes back, so a call is attributed to the agent that made it. The panel sends none of that; it is the user, and there is only one.
