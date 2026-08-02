# weave

Weave is a terminal-based agent manager. It lets you:

- Quickly create agents backed by different CLIs, like Claude or Codex
- Search and act upon agents from one unified view
- Group agents into workflows with common behaviors and settings
- Build custom actions using agent state
- Schedule agents using cron-like syntax
- Build "orchestrator" agents or agent pipelines using Weave's MCP

## Why Weave

Running multiple agent sessions is not difficult, but keeping track of them is. Weave's persistent tmux panel (`prefix + g`) lets you browse and act upon them and related resources more easily.

Weave is different from existing solutions like Conductor or Happy in a few important ways:

1. It is open-source, with a permissive MIT license.
2. It terminal (tmux) based.
3. It is unopinionated. Weave provides the primitives (workflows, agents, schedules, actions, and an MCP layer) that enable agentic automation. You decide how to use them.

## Quickstart

1. Weave stores it's state in Postgres. You can configure it by setting a `DATABASE_URL` in your configuration file.
2. Install the tmux plugin. With TPM, add to `~/.tmux.conf`, then press `prefix + I`:

```tmux
set -g @plugin 'git@github.com:harrisoncramer/weave'
```

3. Press `prefix + g` to toggle the panel. Use `new agent` to launch a background agent, or `W` to create a new workflow. Press `prefix + g` to hide the panel.

See [installation](docs/install.md) for the full setup guide.

## Documentation

Concepts and guides:

- [How it works](docs/concepts.md): the state model, the store and server, the reconciler, and dormant agents
- [The panel](docs/panel.md): the popup, the picker, and the agents browser
- [Commands](docs/commands.md): what `serve` and `migrate` are for
- [Installation](docs/install.md): the tmux plugin setup
- [Configuration](docs/configuration.md): environment variables and the TOML app config
- [Development](docs/development.md): mise tasks, the live rebuild loop, generated code, and the custom linter

Command reference, with the usage line and every flag for each command:

- [reference/](docs/reference/weave.md)
