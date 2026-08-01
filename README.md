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

## The read-only database role

Weave hands every agent an MCP tool that runs SELECT statements against its own database. What such a statement can reach is decided by the role it runs as, not by the tool, because Postgres exempts a superuser from the privilege checks on the catalogs and on the file-reading functions and those are reached through an ordinary SELECT. So the query tools connect as a login of their own that holds SELECT on weave's tables and nothing else.

On a local checkout there is nothing to do. `mise run docker:up` creates the role and grants it, and the default `WEAVE_READONLY_DATABASE_URL` already points at it.

If you set `DATABASE_URL` to your own Postgres, set `WEAVE_READONLY_DATABASE_URL` to the login you want the query tools to use, then run `mise run db:readonly-role` once against it. Run it again after a release that adds tables if you provisioned the role before those tables existed.

Leaving `WEAVE_READONLY_DATABASE_URL` unset falls back to `DATABASE_URL`, which keeps working and writes a warning to the log naming what it costs.

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
