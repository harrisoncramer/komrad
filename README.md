# tmux-claude-agents

A tmux panel for background Claude Code agents running across your tmux sessions.

Each Claude session reports its state (working, done) into a small registry. tmux shows a live global summary in the status bar, and a popup panel (`prefix + g`) lets you pick any agent and jump to it, open its live chat in a large popup, or send it a message. You step away, and you can see and reach every agent from anywhere.

## What it does

- Status bar shows a global count across all sessions, for example `1 working  1 done`.
- `prefix + g` opens a small centered popup listing every live agent with its state and its last "done" message.
- Pick an agent, then pick an action:
  - jump: switch to that agent's session and pane
  - chat: open the agent's live session in a large 90% popup so you can talk to it without leaving your current view (close it with `prefix + d` to detach)
  - destroy: kill that agent's session (with a confirm); if you are in it, you get bounced back to the previous session first

The picker also has a `new chat` entry (prompts for a name, opens a session in the current dir and launches claude) and a `last window/session` entry.

## How state is tracked

State lives in a registry directory, `~/.claude/agents` by default (override with `CLAUDE_AGENTS_DIR`). Two Claude Code lifecycle hooks maintain it:

- On each prompt, the agent is marked `thinking` (counts as working).
- On each stop, the agent is marked `done`. If the agent called `claude-notify` during the turn, its custom message is kept.

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

To let an agent attach a custom "done" message, have it run as its final step:

```
$HOME/.tmux/plugins/tmux-claude-agents/scripts/claude-notify "one line describing what finished"
```

A Claude Code skill that instructs the agent to do this is a good companion; see the plugin's skill example.

## Requirements

- tmux
- Go (to build the binary; the plugin builds it on first load)

## Architecture

Everything is one Go binary, `claude-agents`, with subcommands the plugin and the Claude Code hooks call:

- `panel` — the bubbletea UI (`prefix + g`)
- `notify` — record a finished agent with a message
- `status` — the status-bar aggregate
- `hook prompt|stop` — the Claude Code lifecycle hooks
- `chat` / `chat-open` — the large attach popup

The code is split by domain: `internal/tmux` (a tmux client), `internal/registry` (the agent store), `internal/agents` (the AgentManager wiring the two together), `internal/panel` (the UI), `config` (ultra-loaded settings), and `lib/xstr` (generic helpers). The `scripts/` files are thin wrappers over the binary so the plugin and hook paths stay stable.

## Configuration

Customization is a TOML file loaded via viper, at `~/.config/claude-agents/config.toml` (or `--config <path>`). See `config.example.toml`.

- `claude_flags` — default flags appended to `claude` for every new session (the new-session form can add more).
- `[hooks]` — paths to lifecycle scripts. `pre_create` runs before launch with the session name and directory, and whatever it prints on stdout becomes the directory claude runs in. This is how a new session can spin up a git worktree (e.g. via a `wtc` script) and launch claude inside it. `post_create` and `on_delete` fire around the rest of the lifecycle.

The new-session form also has a `Directory` field: type a target dir, and the `pre_create` hook receives it along with the session name.

The CLI is cobra-based; `claude-agents --help` lists the subcommands (`new`, `notify`, `status`, `hook`, `chat`, `chat-open`). Running it with no subcommand opens the panel.

## Development

The Go module is at the repo root; the custom linter is under `tools/`. Managed with mise.

- `mise run go:build` builds the binary
- `mise run go:lint` builds the custom golangci-lint (with this repo's plugins) if needed and runs it
- `mise run go:build-linter` rebuilds that custom binary
- `mise run go:test` runs the tests
- `mise run go:fmt` / `mise run go:tidy`

Configuration is read via ultra in `go/config`; do not call `os.Getenv` directly. The custom linter under `tools/agentslinter` enforces this with a `forbidgetenv` analyzer.
