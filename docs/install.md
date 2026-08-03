# Installing weave

Weave runs coding agents in tmux panes on a tmux server of its own, and gives you one panel to watch them from. It all runs on your machine and there is nothing to sign up for.

Weave is not published anywhere yet. No releases, no Homebrew tap, and the Go module is still named `weave` rather than a repository URL, so `go install` cannot resolve it. Building from a checkout is the only path that works today.

## Requirements

- A Mac or a Linux machine. Weave shells out to tmux for everything, so there is no Windows build.
- tmux 3.3 or newer. The panel is a `display-popup` and the binding sets its title with `-T`. Check with `tmux -V`.
- A POSIX shell as tmux's `default-shell`. Weave types command lines into panes, and fish and nushell parse them differently, so the server refuses to start rather than half-launching agents. `set -g default-shell /bin/sh` is enough and does not change the shell you get in a normal pane.
- PostgreSQL 18 or newer. The schema defaults primary keys to `uuidv7()`, which does not exist before 18, and transcript search needs `pg_trgm`. The database is yours; weave migrates the schema inside it.
- At least one agent CLI on your `PATH`, already logged in. Weave drives `claude` and `codex` and does not manage their credentials.
- Go 1.25.1 or newer, to build.

## Building from a checkout

```bash
git clone git@github.com:harrisoncramer/weave.git ~/weave
cd ~/weave
mise install
mise run go:build
```

That leaves the binary at `bin/weave`. [mise](https://mise.jdx.dev) pins the toolchain and carries the task tree; see [development](development.md).

If you have no Postgres, the checkout ships one:

```bash
mise run docker:up
```

That starts Postgres 18 on port 5460, applies the migrations, and creates the read-only login. 5460 is what the built-in `DATABASE_URL` default points at, so a checkout with the compose database up needs no environment at all. Then `mise run panel`, or `prefix + g` once the plugin is wired up.

Two checkouts on one machine will keep restarting each other's service, because the panel refuses to talk to a server built from a different version. Give them separate addresses with `WEAVE_ADDR`.

## The tmux plugin

Not really an install channel, it is the keybinding, and you want it either way. With [tpm](https://github.com/tmux-plugins/tpm), add this and press `prefix + I`:

```tmux
set -g @plugin 'harrisoncramer/weave'
```

Without tpm, `run-shell /path/to/weave/weave.tmux`.

The binding is `prefix + g`: press to open, press again to hide. Hiding is not quitting, the panel and every agent keep running. Rebind with `set -g @weave_key 'w'` before the plugin line.

One warning about the current script: it hardcodes the binary to `bin/weave` inside the plugin directory and builds it with `go build ... || true`, so a machine with no Go toolchain gets a silent no-op and a key bound to nothing. If `prefix + g` does nothing useful, check that first.

## Channels that are not live yet

Homebrew is the planned primary channel, because a cask can depend on `tmux` and `postgresql@18` and nothing else here installs weave's dependencies for you. It will need `brew trust harrisoncramer/tap` first, since Homebrew 6.0 made third-party taps untrusted and a later `brew upgrade` silently skips untrusted ones. The tap prefix matters too: `weave` in homebrew/core is somebody else's tree-sitter merge driver.

Prebuilt binaries will come from GoReleaser on a tag, for darwin and linux on amd64 and arm64, with a `checksums.txt`. The checksums come from the same host as the archive, so they defend against a corrupted download and not a compromised release.

`go install github.com/harrisoncramer/weave@latest` works once the module is renamed. It gives you a binary and nothing else: no plugin, no Postgres, no database.

## After installing

Only a checkout with `mise run docker:up` skips this.

Point weave at a database. One connection string:

```bash
export DATABASE_URL='postgres://weave:secret@localhost:5432/weave?sslmode=disable'
```

Weave migrates the schema through it, so an empty database is all you have to create. It connects twice, though: the [MCP](mcp.md) query tools use a least-privilege login, by default that same URL with `weave_readonly` in place of yours, and weave refuses to start if that login is a superuser. Set `WEAVE_READONLY_DATABASE_URL` when it needs its own password or reaches the database another way; see [configuration](configuration.md).

The built-in default points at port 5460, which is what the repo's docker-compose publishes and wrong for a brew-installed Postgres on 5432, so skipping this step elsewhere fails against a port nothing is on.

Create the read-only login:

```bash
weave readonly-role
```

It connects as `DATABASE_URL`, needs `CREATEROLE`, and grants read on the application tables and nothing else, including the ones later migrations add. Run it after the schema exists. Do not skip it: the service opens that pool during startup and aborts if it cannot, so everything else can be right and the panel still will not open. Running it again is free.

Add the tmux plugin line, start the service with `weave start`, and check the machine:

```bash
weave doctor
```

Doctor runs one check per dependency, prints one line each, and repairs nothing. Failing lines carry the fix underneath, and it exits non-zero when something will stop the panel working, so it scripts. `--json` prints the raw report.

```
  ok   tmux             /etc/profiles/per-user/you/bin/tmux
  ok   tmux version     tmux 3.7b
  ok   tmux shell       /bin/zsh (weave's tmux server)
  ok   config           /Users/you/.config/weave/config.toml
  ok   default harness  claude
  ok   database         postgres://weave@localhost:5460/weave
  ok   schema           up to date
  ok   readonly login   postgres://weave_readonly@localhost:5460/weave
  ok   server           127.0.0.1:8787 serving v0.1.0
  ok   claude           /Users/you/.local/bin/claude
  ok   codex            /etc/profiles/per-user/you/bin/codex

11 checks, all ok.
```

Then, inside tmux, press `prefix + g`.

## When it does not work

Run `weave doctor` first. Most of what goes wrong on a fresh install is one of its lines.

The service log is next, at `~/Library/Caches/weave/weave.log` on a Mac and `~/.cache/weave/weave.log` on Linux, one JSON record per line. Anything that failed at startup is explained there.

A popup that flashes and vanishes takes its error with it. Run `weave open` by hand to see what it said.

If `weave version` reports one build for the binary and another for the server, something is still running the old one: `weave stop` then `weave start`. If the panel itself is stale, `tmux -L weave kill-server` and press the key again. That kills your agents' panes; their records and transcripts survive in Postgres.

One that catches people: a tmux server hands every session the environment it was started with, so exporting a new `DATABASE_URL` and pressing `prefix + g` will not reach a panel session that is already up. Restart the tmux server as above.

From here, [configuration](configuration.md) covers the config file and environment, and [commands](commands.md) covers the CLI.
