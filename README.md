# tmux-claude-agents

A tmux panel for background Claude Code agents running across your tmux sessions.

The tool watches each agent's state, thinking, waiting, or done, and keeps a small store. A persistent popup panel (`prefix + g`) lets you browse every agent, watch a live preview of its pane, and open any one in place. You step away, and you can still see and reach every agent from anywhere.

NOTE: This project is still under development. Expect breaking changes.

## Quickstart

The store is Postgres. In development it runs in Docker:

```
mise run docker:up
```

That starts the database and applies the migrations. Anywhere else, point `DATABASE_URL` at your own instance and the tool migrates it on first run.

1. Install the tmux plugin. With TPM, add to `~/.tmux.conf`, then press `prefix + I`:

```
set -g @plugin 'git@github.com:harrisoncramer/tmux-claude-agents'
```

2. Press `prefix + g` to toggle the panel. Use `new agent` to launch a background agent, or `agents (N)` to browse the ones you have. `prefix + g` hides the panel again from any view and brings it back exactly as you left it.

There is nothing else to configure. The tool works out each agent's state on its own.

The panel and its agents live on a tmux server of their own, not the one your own sessions are on. A tmux server only lists its own sessions, so the panel's session stays out of your session list and out of the switcher, and the only way to it is the popup, which is the only place it is meant to be seen. Point `CLAUDE_AGENTS_TMUX_SOCKET` somewhere else if you want that server on a different socket.

Upgrading from a version before that split leaves a `claude-manager` session and a `serve` process behind on your own tmux server. Kill both once and reopen the panel. Left running, the old server keeps answering, and agents you create land on it rather than the panel's.

See [installation](docs/install.md) for the full setup, including the panel key.

## Why

Running several Claude Code sessions at once is easy. Keeping track of them is not. Once you switch away from a session you lose sight of whether it is still working or has stopped and is waiting on you, and getting back to the right one means cycling through tmux sessions by hand.

This tool gives you one place for all of it. It works out each agent's state by watching its tmux pane, so there is nothing to set up and nothing to report, it just observes. The popup lets you preview and jump into any agent without leaving where you are. Agent records outlive their tmux sessions, so a finished or restarted agent still lists, and opening it relaunches Claude with `--resume` so the conversation continues.

## Documentation

Concepts and guides:

- [How it works](docs/concepts.md): the state model, the store and server, the reconciler, and dormant agents
- [The panel](docs/panel.md): the popup, the picker, and the agents browser
- [Commands](docs/commands.md): what `serve` and `migrate` are for
- [Installation](docs/install.md): the tmux plugin setup
- [Configuration](docs/configuration.md): environment variables and the TOML app config
- [Development](docs/development.md): mise tasks, the live rebuild loop, generated code, and the custom linter

Command reference, with the usage line and every flag for each command:

- [reference/](docs/reference/claude-agents.md)
