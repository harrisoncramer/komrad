# weave

Weave is a Tmux-first workflow for managing coding agents. With weave you can:

- Create agents backed by different backends, like Claude or Codex
- Quickly search, archive, restore agent sessions behind
- Group agents into workflows with predefined context and working directories
- Schedule agents and workflows
- Manage your agents with another Agent through weave's MCP layer

Running several agent sessions across terminal sesssions is not hard. But keeping track of them, prodding them along, and searching through old or expired sessions is not.

Weave's tmux panel helps you track and manage each agent's state, and come back to them as necessary. The persistent panel (`prefix + g`) lets you browse resources and open the conversations on the fly.

## Quickstart

1. Weave stores it's state in Postgres. You can configure it by setting a `DATABASE_URL` in your configuration file.
1. Install the tmux plugin. With TPM, add to `~/.tmux.conf`, then press `prefix + I`:

```tmux
set -g @plugin 'git@github.com:harrisoncramer/weave'
```

3. Press `prefix + g` to toggle the panel. Use `new agent` to launch a background agent, or `W` to create a new workflow. Press `prefix + g` to hide the panel.

See [installation](docs/install.md) for the full setup, including the panel key.

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
