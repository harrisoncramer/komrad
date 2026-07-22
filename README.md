# tmux-claude-agents

A tmux panel for background Claude Code agents running across your tmux sessions.

Each Claude session reports its state, thinking or done, into a small store. tmux shows a live global summary in the status bar, and a persistent popup panel (`prefix + g`) lets you browse every agent, watch a live preview of its pane, and open any one in place. You step away, and you can still see and reach every agent from anywhere.

NOTE: This project is still under development. Expect breaking changes.

## Quickstart

1. Install the tmux plugin. With TPM, add to `~/.tmux.conf`, then press `prefix + I`:

```
set -g @plugin 'git@github.com:harrisoncramer/tmux-claude-agents'
set -g status-right "#{claude_agents}%I:%M:%S "
```

2. Wire the Claude Code hooks in `settings.json` so each agent reports its state:

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

3. Press `prefix + g` to open the panel. Use `new chat` to launch a background agent, or `chats (N)` to browse the ones you have.

See [installation](docs/install.md) for the full setup, including the panel key and the notify command.

## Why

Running several Claude Code sessions at once is easy. Keeping track of them is not. Once you switch away from a session you lose sight of whether it is still working or has stopped and is waiting on you, and getting back to the right one means cycling through tmux sessions by hand.

This tool gives you one place for all of it. Each agent reports its own state through Claude Code's lifecycle hooks, so the panel never guesses; it shows what the agents have already told it. The status bar keeps a running count across every session, and the popup lets you preview and jump into any agent without leaving where you are. Agent records outlive their tmux sessions, so a finished or restarted agent still lists, and opening it relaunches Claude with `--resume` so the conversation continues.

## Documentation

Concepts and guides:

- [How it works](docs/concepts.md): the state model, the store and server, hooks, and dormant agents
- [The panel](docs/panel.md): the popup, the picker, and the chats browser
- [Commands](docs/commands.md): what `serve`, `new`, `notify`, `status`, and `hook` are for
- [Installation](docs/install.md): the tmux plugin and the Claude Code hooks
- [Configuration](docs/configuration.md): environment variables and the TOML app config
- [Development](docs/development.md): mise tasks, generated code, and the custom linter

Command reference, with the usage line and every flag for each command:

- [reference/](docs/reference/claude-agents.md)
