# How it works

You run several Claude Code sessions at once, each in its own tmux session, each working on something different. The problem is keeping track of them. Once you switch away from a session you lose sight of whether it is still working or has stopped and is waiting on you. This tool gives you one place to see every agent and reach any of them.

The design is that the tool observes each agent's state itself rather than waiting to be told. A single background process watches every agent it launched and works out what each one is doing by looking at it. The panel just displays what that process has recorded.

## The store and the server

State lives in a SQLite database at `~/.claude/claude-agents.db` by default. You can point it elsewhere with `CLAUDE_AGENTS_DB`.

The `serve` command runs a small ConnectRPC server that owns the database and reads state over HTTP. It also runs the reconciler described below. The panel spawns that server automatically the first time it needs it, so you never start it by hand.

## How state is worked out

The serve process runs a reconciler on a short timer, and every couple of seconds it looks at each agent and writes the state that observation implies. Because it reads the ground truth every pass, a wrong state cannot last longer than one tick, and there is no event for it to miss.

For each agent, the reconciler captures its tmux pane and reads it:

If the pane shows Claude's working line, the spinner and elapsed timer it displays while generating or running a tool, the agent is `thinking`. This is immediate: the moment work appears, the agent counts as working.

If the pane shows an interactive prompt, the footer Claude puts under a permission dialog or a multiple-choice question, the agent is `waiting`. This is the state that matters most when you have stepped away: the agent is not grinding and not finished, it is blocked until you answer. The panel and status bar show it in red.

If the pane shows neither, just the idle input box, an agent that had been working settles to `done`, after a couple of consecutive idle observations so a brief pause between tool calls is not mistaken for a stop. An agent that never started stays `new`, and one already `done` stays done. The done state sticks until work resumes, so a finished agent keeps showing as done until you come back to it.

If the agent's tmux session is gone, it cannot be working, so an active state settles to done.

Only agents the tool created are ever watched, so a Claude session you start yourself is never tracked. The status bar reads the store and shows a single aggregate across every session, for example `1 waiting  1 working  1 done`.

## Dormant agents

An agent's record in the store outlives its tmux session. That is what lets agents survive a tmux restart. When a tmux session is gone but its record remains, the agent still appears in the panel, marked `(dormant)`.

A dormant agent has no live pane to preview, so the panel shows its stored fields instead: the title, directory, state, flags, and prompt. Opening it restores it. A fresh tmux session is created from the stored directory and flags, and Claude relaunches with `--resume` on the agent's session id so the conversation picks up where it left off. That id is chosen by the tool when it first launches the agent, with `--session-id`, so it is always known without anything having to report it.

## Workflows

A workflow is a named bucket of related chats, stored as its own row alongside the sessions. It runs nothing, so unlike an agent it has no pane or state. A chat points at its workflow through a nullable `workflow_id`, where null means the chat belongs to no workflow. Everything keys on the workflow's id; its name is only a display label you can rename freely.

The panel scopes to a workflow when you open one, filtering the chats list, new chat, and search to it, while the root view still lists every chat. Back-navigation follows the scope you were browsing, not the chat's stored workflow: a chat opened inside a workflow returns there on detach, while one opened from the root returns to the root. Deleting a workflow prompts for what happens to its chats: keep them, which orphans them back to unassigned, or delete them, which soft-deletes the chats alongside the workflow while keeping them in search.

## Where the pieces live

Everything is one Go binary, `claude-agents`, driven by cobra. Running it with no subcommand opens the panel. The subcommands are what the plugin's scripts call. See [commands](commands.md) for what each one is for, and [reference/](reference/claude-agents.md) for the exact flags.

The code is split by domain:

- `internal/cli` is the cobra command tree and dependency wiring
- `internal/tmux` is a tmux client
- `internal/store` is the sqlc-backed SQLite session store
- `internal/rpc` is the ConnectRPC `SessionService` handler over the store
- `internal/agents` is the AgentManager, the domain logic wiring tmux and the store together
- `internal/panel` is the bubbletea UI
- `internal/appconfig` is the TOML app config
- `config` is the environment configuration, loaded via ultra
- `proto/` holds the session protobufs and generated Go and ConnectRPC code
- `db/` holds the SQLite schema, Atlas migrations, and sqlc queries and models

The files under `scripts/` are thin wrappers over the binary, so the paths the plugin references stay stable even as the binary changes.
