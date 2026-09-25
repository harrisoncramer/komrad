# komrade

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT" /></a>
</p>

Komrade is an agent manager that lives in your terminal. It runs Claude and Codex agents in tmux, keeps track of them, and lets them work together.

With komrade you can create and manage agents, group them into workflows that share settings, run your own scripts at moments in an agent's life, launch agents on a schedule, and let agents manage other agents through komrade's MCP server.

If you use Neovim or Emacs, or just like working from a terminal, komrade is for you.

<img width="1727" height="1055" alt="The komrade panel" src="https://github.com/user-attachments/assets/9a67ecbc-ff0e-4fd1-a8d1-8dd76dd86a5e" />

## Quickstart

1. Set up a Postgres database and point `connection.database_url` in your komrade config at it.
2. Install komrade by following the [installation guide](https://docs.komrade.dev/latest/guides/install/).
3. Press `prefix + g` to open the panel. Press `w` to create your first workflow, then `c` to add your first agent.

## Why komrade

Keeping track of several agent sessions is hard. Komrade gives you one place to browse, search and talk to all of them, instead of juggling terminal panes.

It is unopinionated. Komrade gives you the building blocks as things you manage directly: [workflows](https://docs.komrade.dev/latest/guides/workflows/), [agents](https://docs.komrade.dev/latest/guides/agents/), [hooks](https://docs.komrade.dev/latest/guides/hooks/) and [schedules](https://docs.komrade.dev/latest/guides/schedules/). Its [MCP server](https://docs.komrade.dev/latest/guides/mcp/) and [CLI](https://docs.komrade.dev/latest/cli/) let agents manage other agents. How you put them together is up to you.

Komrade is built for software work in [git worktrees](https://git-scm.com/docs/git-worktree), but it handles production support, Slack or email work just as well.

It lives in tmux. Komrade is a tool for people who work in the terminal. It is not a remote coding environment or a separate app.

## Documentation

The documentation lives at [docs.komrade.dev](https://docs.komrade.dev/latest/). The guides explain how komrade works and how to use it. The API, CLI and config references are generated from the code, so they always match the build they describe.

The site's source is in [docs/](docs/). Run `mise run docs:dev` to work on it locally.
