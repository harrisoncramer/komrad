---
title: Installing weave
description: Get the binary, point it at Postgres, and open the panel.
group: start
order: 1
---

## Quick start

Weave runs on Mac and Linux. It needs tmux 3.3 or newer, PostgreSQL 18 or newer, and at least one agent CLI on your `PATH` that is already logged in. Weave drives `claude` and `codex`. Tmux's `default-shell` has to be a POSIX shell, and `set -g default-shell /bin/sh` is enough.

```sh
curl -fsSL -o ~/.local/bin/weave \
  https://github.com/harrisoncramer/weave/releases/download/v0.1.0/weave-darwin-arm64
chmod +x ~/.local/bin/weave

echo "run-shell 'weave tmux'" >>~/.tmux.conf
tmux source-file ~/.tmux.conf

export WEAVE_OWNER_DATABASE_URL="postgres://weave:secret@localhost:5432/weave?sslmode=disable"
weave migrate
weave engine ensure laptop
export WEAVE_DATABASE_URL="<the url it printed>"
weave doctor
```

Then press `prefix + g` inside tmux to open the panel.

## Picking a build

There are four builds: `weave-darwin-arm64` for Apple Silicon, `weave-darwin-amd64` for Intel Macs, and `weave-linux-amd64` and `weave-linux-arm64`.

Each comes on two channels. A stable release has a version number and a changelog, and is cut on purpose. The `tip` release is rebuilt from main on every merge, so its URL never changes and it makes no promises. Expect breaking changes and bugs if you run it.

```
https://github.com/harrisoncramer/weave/releases/download/v0.1.0/weave-darwin-arm64
https://github.com/harrisoncramer/weave/releases/download/tip/weave-darwin-arm64
```

While the repository is private, both URLs answer 404 to a plain curl. Use `gh release download` until it is public.

Every release carries a `checksums.txt`. Check a download against it with `shasum -a 256 -c checksums.txt --ignore-missing`.

If you use a remote engine, your weave and the engine's have to be the same build, because the panel refuses to talk to an engine running other code. Install yours from the release the engine runs, and upgrade both together. [Running weave remotely](../remote/) covers this.

## The database

Weave needs an empty database and a login that owns it.

Several engines can share one database. An engine is one running weave on one machine. Each engine connects with a login of its own, and Postgres keeps every engine to its own rows.

The owner login sets things up. `weave migrate` brings the schema up to date. `weave engine ensure NAME` creates an engine with its login and a read-only login for its MCP tools, then prints the URL the engine should connect with. Both commands connect as `owner_database_url`, which falls back to `database_url`.

```toml
[connection]
owner_database_url = "postgres://weave:secret@localhost:5432/weave?sslmode=disable"
database_url       = "postgres://weave_engine_laptop:<password>@localhost:5432/weave?sslmode=disable"
```

The service never migrates on its own. It refuses to start against a schema its build does not match, or as a login that belongs to no engine.

The built-in defaults point at the development Postgres in a checkout, on `localhost:5460`. An installed binary needs these two URLs set.

## Upgrading

Download the new binary over the old one. Your config, database and tmux config are left alone.

The running processes keep the build they started with, so stop the service, migrate, make sure the engine has its logins, and start it again.

```sh
weave stop
weave migrate
weave engine ensure default
weave start
tmux -L weave kill-server
```

Press `prefix + g` for a fresh panel. Killing weave's tmux server stops the agents that were running, but their records and transcripts stay in the database.

A panel that is open when the binary is replaced shows a screen saying so. Any key closes it, and the panel key opens a new one from the new build.

## From a checkout

Contributors build from source, which needs Go 1.25.1 or newer.

```sh
git clone git@github.com:harrisoncramer/weave.git ~/weave
cd ~/weave
mise install
mise run go:build
mise run docker:up
```
