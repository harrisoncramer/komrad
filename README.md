, weave

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT" /></a>
</p>

<hr>

Weave is a terminal-based agent manager. It lets you:

- Quickly create agents backed by different CLIs, like Claude or Codex
- Build orchestrators or agent pipelines with Weave's MCP
- Build custom actions and triggers tied to agent lifecycles
- Group agents into workflows with common behaviors and contexts
- Enforce agent schedules

If you're a Neovim or Emacs user, or just enjoy working from your terminal, weave is for you.

## Quickstart

1. Configure a Postgres database and set `connection.database_url` in your weave [configuration file](docs/configuration.md).
2. Install weave. See [installation](docs/install.md) for the full setup guide.
3. Press `prefix + g` to toggle the agent manager panel. Press `w` to create your first workflow, and then `a` to add your first agent.

## Why Weave

Keeping track of multiple agent sessions is difficult. Weave's persistent agent manager lets you browse, search, and interact with them more easily than juggling multiple terminal panels.

Weave is different from existing solutions like Conductor, Happy, or WorkMux in a few important ways:

1. It is unopinionated.

Weave provides the primitives behind agentic workflows as resources that you manage directly: [workflows](docs/workflows.md), [agents](docs/agents.md), [actions](docs/actions.md), and [schedules](docs/schedules.md). It also provides an [MCP layer](docs/mcp.md) and a [CLI](docs/commands.md) so that you can have weave agents manage other agents. But it doesn't tell you how to use them: that's your call.

Weave is optimized for software development with [https://git-scm.com/docs/git-worktree](worktrees), but is equally capable of handling other automations, like production incidents or triaging, or even Slack or email automations. Let weave automate the parts of your job you dislike.

2. It terminal (tmux) based.

Weave is a terminal tool, intended for power users. It does not aspire to be a remote coding environment or a standalone application, and meets you in the terminal.

## Documentation

Concepts and guides:

- [Installation](docs/install.md): the tmux plugin setup
- [Configuration](docs/configuration.md): environment variables and the TOML app config
- [Development](docs/development.md): mise tasks, the live rebuild loop, generated code, and the custom linter
- [How it works](docs/concepts.md): the state model, the store and server, the reconciler, and agents

Terminology:

- [workflows](docs/workflows.md): The shared templates for agents
- [agents](docs/agents.md): The core resource of weave
- [actions](docs/actions.md): Custom scripts or binaries that your agents can invoke
- [schedules](docs/schedules.md): Durable schedules for running your workflows at prefined times or intervals
- [MCP](docs/mcp.md): Lets agents spawn or manage other agents
- [CLI](docs/commands.md): The CLI tool and commands
