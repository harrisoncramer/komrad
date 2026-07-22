# tmux-claude-agents

A tmux panel for background Claude Code agents running across your tmux sessions.

Each Claude session reports its state (thinking, done) into a small store. tmux shows a live global summary in the status bar, and a persistent popup panel (`prefix + g`) lets you browse every agent, watch a live preview of its pane, and open any one in place. You step away, and you can see and reach every agent from anywhere.

## What it does

- Status bar shows a global count across all sessions, for example `1 working  1 done`.
- `prefix + g` opens the `claude-manager` popup (a 95% centered popup) with the picker.
- The picker has two rows:
  - `new chat` — prompts for a title, directory, flags, and an opening prompt, then launches a background claude session
  - `chats (N)` — the split browser: an agent list on the left, a live preview of the hovered agent's pane on the right
- In the chats browser:
  - `enter` / `l` — open that agent in place, inside the same popup; detach (`prefix + d`) to return to the picker
  - `r` — rename the agent (renames the tmux session too)
  - `d` — delete the agent (soft delete; hides it and kills the session). If you are in it, you get bounced to the previous session first
  - `j` / `k` — move; `C-d` / `C-u` — scroll the preview; `esc` — back to the picker

The popup is a persistent space. Opening an agent attaches to it without closing the popup; detaching drops you back at the picker. Pressing the panel key while already viewing an agent detaches back to the picker instead of nesting another popup. The popup closes only when you quit the picker.

## Dormant agents

An agent's record outlives its tmux session, so agents survive tmux restarts. A dormant agent (its pane is gone) still lists, shown as `(dormant)`. Its preview shows the stored title, directory, state, flags, and prompt instead of a live capture. Pressing `enter` restores it: a fresh tmux session is created from the stored directory and flags, and if Claude's own session id was recorded, claude relaunches with `--resume` so the conversation continues.

## How state is tracked

State lives in a SQLite database at `~/.claude/claude-agents.db` by default (override with `CLAUDE_AGENTS_DB`). The panel talks to it over ConnectRPC: a background `serve` process owns the database and the panel is a client. The panel auto-spawns that server on first use if it is not already reachable.

Two Claude Code lifecycle hooks advance an agent's state:

- On each prompt, the agent is marked `thinking` (counts as working) and Claude's session id is recorded for later `--resume`.
- On each stop, the agent is marked `done` (unless it is already done).

The done state persists until you send that agent a new prompt, so a finished agent keeps showing until you come back to it.

## Install (tmux side)

With TPM, add to `~/.tmux.conf`:

```
set -g @plugin 'git@github.com:harrisoncramer/tmux-claude-agents'
```

(SSH form because the repo is private; use `harrisoncramer/tmux-claude-agents` if it becomes public.)

Put the status placeholder wherever you want the summary to appear, for example:

```
set -g status-right "#{claude_agents}%I:%M:%S "
```

Then `prefix + I` to install. The panel key defaults to `g`; change it with:

```
set -g @claude_agents_key 'g'
```

## Install (Claude Code side)

The status hooks and the notify command ship in `scripts/`. Wire the hooks in Claude Code `settings.json`, pointing at the plugin path:

```
"hooks": {
  "UserPromptSubmit": [
    { "matcher": "", "hooks": [ { "type": "command", "command": "$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-tmux-status.sh prompt" } ] }
  ],
  "Stop": [
    { "matcher": "", "hooks": [ { "type": "command", "command": "$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-tmux-status.sh stop" } ] }
  ]
}
```

To mark an agent done from within a turn, have it run as its final step:

```
$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-notify "one line describing what finished"
```

The `notify-when-done` skill under `skill/` instructs an agent to do this.

## Requirements

- tmux
- Go (to build the binary; the plugin builds it on first load)

## Architecture

Everything is one Go binary, `claude-agents`, driven by cobra. Running it with no subcommand opens the panel; the subcommands the plugin and the Claude Code hooks call are:

- `serve` — run the ConnectRPC/HTTP session server that owns the database
- `new` — create a background agent session non-interactively
- `notify` — mark this agent done
- `status` — the status-bar aggregate
- `hook <prompt|stop>` — advance agent state from a Claude Code lifecycle event

The code is split by domain:

- `internal/cli` — the cobra command tree and dependency wiring
- `internal/tmux` — a tmux client
- `internal/store` — the sqlc-backed SQLite session store
- `internal/rpc` — the ConnectRPC `SessionService` handler over the store
- `internal/agents` — the AgentManager, the domain logic wiring tmux and the store together
- `internal/panel` — the bubbletea UI
- `internal/appconfig` — the TOML app config (flags, hooks)
- `config` — environment configuration (ultra-loaded)
- `lib/xstr` — generic string helpers
- `proto/` — the session protobufs and generated Go/ConnectRPC code
- `db/` — the SQLite schema, Atlas migrations, and sqlc queries and models

The `scripts/` files are thin wrappers over the binary so the plugin and hook paths stay stable.

## Configuration

There are two layers.

Environment (loaded via ultra in `config`):

- `CLAUDE_AGENTS_DB` — database path (default `~/.claude/claude-agents.db`)
- `CLAUDE_AGENTS_ADDR` — session server listen address (default `127.0.0.1:8787`)
- `CLAUDE_AGENTS_TMUX_SOCKET` — tmux socket (default `default`)
- `MAIN_SESSION` / `TMUX_PANE` — identify the calling agent; set by the popup and tmux

App config, a TOML file loaded via `internal/appconfig` at `~/.config/claude-agents/config.toml` (or `--config <path>`). See `config.example.toml`.

- `claude_flags` — default flags appended to `claude` for every new session (the new-chat form can add more).
- `[hooks]` — paths to lifecycle scripts. `pre_create` runs before launch with the session name and directory, and whatever it prints on stdout becomes the directory claude runs in. This is how a new session can spin up a git worktree (e.g. via a `wtc` script) and launch claude inside it. `post_create` and `on_delete` fire around the rest of the lifecycle.

The new-chat form also has a `Directory` field: type a target dir, and the `pre_create` hook receives it along with the session name.

## Development

The Go module is at the repo root; the custom linter is under `tools/`. Managed with mise.

- `mise run go:build` builds the binary
- `mise run go:lint` builds the custom golangci-lint (with this repo's plugins) if needed and runs it
- `mise run go:build-linter` rebuilds that custom binary
- `mise run go:test` runs the tests
- `mise run go:fmt` / `mise run go:tidy`
- `mise run proto:generate` regenerates the Go/ConnectRPC code from `proto/`
- `mise run db:migrate <name>` authors an Atlas migration by diffing `db/schema`
- `mise run db:sqlc` regenerates the sqlc query bindings against the migrations
- `mise run db:connect` opens a `sqlite3` shell to the database

Configuration is read via ultra in `config`; do not call `os.Getenv` directly. The custom linter under `tools/agentslinter` enforces this with a `forbidgetenv` analyzer.
