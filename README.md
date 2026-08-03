# weave

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT" /></a>
</p>

<hr>

Weave is a terminal-based agent manager. It lets you:

- Quickly create agents backed by different CLIs, like Claude or Codex
- Search and act upon agents from one unified view
- Group agents into workflows with common behaviors and settings
- Build custom actions using agent state
- Schedule agents
- Build orchestrator agents or agent pipelines using Weave's MCP

If you're a Neovim or Emacs user, love tools like FZF, or just enjoy working from your terminal, weave is for you.

## Quickstart

1. Configure a Postgres database and set a `DATABASE_URL` in your weave configuration file.
2. Install the tmux plugin. With TPM, add to `~/.tmux.conf`, and press your installation command, typically `prefix + I`:

```tmux
set -g @plugin 'git@github.com:harrisoncramer/weave'
```

3. Press `prefix + g` to toggle the agent manager panel. Press `w` to create your first workflow, and then `w` to add your first agent.

See [installation](docs/install.md) for the full setup guide.

## Why Weave

Keeping track of multiple agent sessions is difficult. Weave's persistent tmux panel (`prefix + g`) lets you browse, search, andw interact with them more easily than juggling multiple terminal panels. Weave is different from existing solutions like Conductor, Happy, or WorkMux in a few important ways:

1. It is unopinionated.

Weave provides the primitives behind agentic workflows: agents, workflows, actions, schedules, and an MCP layer. You decide how to use them.

Weave is an excellent software development environment using Git worktrees. But it's also equally capable of handling other automations: bug reports, Slack messages, or emails. Let weave take over the parts of your job you dislike, not the parts you do.

2. It terminal (tmux) based.

Weave's purpose is narrow: providing an a local agentic platform for terminal power users. It does not aspire to be a remote coding environment.

If I'm away from my computer, it's because I don't want to be thinking about work.

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
