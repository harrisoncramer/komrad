# How it works

You run several Claude Code sessions at once, each in its own tmux session, each working on something different. The problem is keeping track of them. Once you switch away from a session you lose sight of whether it is still working or has stopped and is waiting on you. This tool gives you one place to see every agent and reach any of them.

The design is that each Claude session reports its own state into a shared store, and a single panel reads that store and shows you everything. Claude tells the store when it starts thinking and when it stops. The panel never has to guess; it just displays what the agents have already reported.

## The store and the server

State lives in a SQLite database at `~/.claude/claude-agents.db` by default. You can point it elsewhere with `CLAUDE_AGENTS_DB`.

Nothing talks to that database file directly except one process. The `serve` command runs a small ConnectRPC server that owns the database, and everything else, the panel and the hooks, is a client that talks to it over HTTP. This keeps a single writer and avoids several processes fighting over the same SQLite file. The panel spawns that server automatically the first time it needs it, so you never start it by hand.

## How state advances

An agent's state changes through four Claude Code lifecycle hooks. All are thin calls into the `hook` subcommand.

When you submit a prompt, the prompt hook marks that agent `thinking`, which counts as working, and records Claude's own session id. That recorded id is what lets a dormant agent later relaunch with `--resume` and keep its conversation.

When Claude needs something from you, a permission it wants granted or any other block, it emits a notification. The notification hook marks that agent `waiting`. This is the state that matters most when you have stepped away: the agent is not grinding and it is not finished, it is stopped and cannot go on until you answer. The panel and status bar show it in red so it stands out from a working agent.

Once you answer and Claude runs its next tool, the post-tool-use hook moves a waiting agent back to `thinking`, so the red clears the moment work resumes rather than waiting for the agent to finish. That hook only touches an agent that was waiting; when the agent is already thinking it does nothing, so the constant stream of tool calls costs a single cheap read each.

When Claude stops, the stop hook marks that agent `done`, unless it is already done. The done state sticks until you send that agent a new prompt. That is deliberate: a finished agent keeps showing as done so you notice it and come back to it, rather than quietly resetting.

The status bar reads the store and shows a single aggregate across every session, for example `1 waiting  1 working  1 done`.

## Reconciliation

The hooks only advance state when an event fires. Some things that end an agent's work fire no event: interrupting Claude with escape, a crash, or a tmux session killed from outside the panel. Any of these leaves the last recorded state frozen, most often an agent stuck working forever because the prompt hook set thinking and no later hook ever ran.

To catch that, the serve process runs a reconciler on a timer. Every few seconds it looks at every agent and asks whether the stored state still matches reality. If an agent's tmux session is gone, it is dormant and cannot be working, so it is settled to done. If an agent is marked thinking but its pane shows no work in flight, it is settled to done as well, after a couple of consecutive idle observations so a brief pause between tool calls is not mistaken for a stop. Waiting is left alone, since the notification and post-tool-use hooks set and clear it on events that do fire.

The reconciler lives in serve rather than the panel because serve is always running and owns the store, so the counts stay correct even when no panel is open. It only ever corrects a state that drifted; an agent that already matches reality is left untouched.

## Dormant agents

An agent's record in the store outlives its tmux session. That is what lets agents survive a tmux restart. When a tmux session is gone but its record remains, the agent still appears in the panel, marked `(dormant)`.

A dormant agent has no live pane to preview, so the panel shows its stored fields instead: the title, directory, state, flags, and prompt. Opening it restores it. A fresh tmux session is created from the stored directory and flags, and if Claude's session id was recorded, Claude relaunches with `--resume` so the conversation picks up where it left off.

## Where the pieces live

Everything is one Go binary, `claude-agents`, driven by cobra. Running it with no subcommand opens the panel. The subcommands are what the plugin and the Claude Code hooks call. See [commands](commands.md) for what each one is for, and [reference/](reference/claude-agents.md) for the exact flags.

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

The files under `scripts/` are thin wrappers over the binary, so the paths the plugin and hooks reference stay stable even as the binary changes.
