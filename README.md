# tmux-claude-agents

A tmux panel for background Claude Code agents running across your tmux sessions.

The tool watches each agent's state, thinking, waiting, or done, and keeps a small store. tmux shows a live global summary in the status bar, and a persistent popup panel (`prefix + g`) lets you browse every agent, watch a live preview of its pane, and open any one in place. You step away, and you can still see and reach every agent from anywhere.

NOTE: This project is still under development. Expect breaking changes.

## Quickstart

1. Install the tmux plugin. With TPM, add to `~/.tmux.conf`, then press `prefix + I`:

```
set -g @plugin 'git@github.com:harrisoncramer/tmux-claude-agents'
set -g status-right "#{claude_agents}%I:%M:%S "
```

2. Press `prefix + g` to open the panel. Use `new chat` to launch a background agent, or `chats (N)` to browse the ones you have.

There is nothing else to configure. The tool works out each agent's state on its own.

See [installation](docs/install.md) for the full setup, including the panel key and the notify command.

## Why

Running several Claude Code sessions at once is easy. Keeping track of them is not. Once you switch away from a session you lose sight of whether it is still working or has stopped and is waiting on you, and getting back to the right one means cycling through tmux sessions by hand.

This tool gives you one place for all of it. It works out each agent's state by watching its tmux pane, so there is nothing to set up and nothing to report, it just observes. The status bar keeps a running count across every session, and the popup lets you preview and jump into any agent without leaving where you are. Agent records outlive their tmux sessions, so a finished or restarted agent still lists, and opening it relaunches Claude with `--resume` so the conversation continues.

## Documentation

Concepts and guides:

- [How it works](docs/concepts.md): the state model, the store and server, the reconciler, and dormant agents
- [The panel](docs/panel.md): the popup, the picker, and the chats browser
- [Commands](docs/commands.md): what `serve`, `new`, `notify`, and `status` are for
- [Installation](docs/install.md): the tmux plugin setup
- [Configuration](docs/configuration.md): environment variables and the TOML app config
- [Development](docs/development.md): mise tasks, the live rebuild loop, generated code, and the custom linter

Command reference, with the usage line and every flag for each command:

- [reference/](docs/reference/claude-agents.md)
