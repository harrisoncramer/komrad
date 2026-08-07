# Installing weave

Weave runs coding agents in tmux panes on a tmux server of its own, and gives you one panel to watch them from. It all runs on your machine.

Weave is one statically linked binary.

## Quick start

```sh
curl -fsSL -o ~/.local/bin/weave \
  https://github.com/harrisoncramer/weave/releases/download/tip/weave-darwin-arm64
chmod +x ~/.local/bin/weave

echo "run-shell 'weave tmux'" >>~/.tmux.conf
tmux source-file ~/.tmux.conf

export WEAVE_DATABASE_URL="postgres://weave:secret@localhost:5432/weave?sslmode=disable"
weave readonly-role
weave doctor
```

Then press `prefix + g` inside tmux.

## Requirements

- Mac or Linux. Weave shells out to tmux for everything, so there is no Windows build.
- tmux 3.3 or newer
- PostgreSQL 18 or newer, reachable from your machine
- A POSIX shell as tmux's `default-shell`. `set -g default-shell /bin/sh` is enough.
- At least one agent CLI on your `PATH`, already logged in. Weave drives `claude` and `codex`.

## The binary

Four builds, published to one release tagged `tip` on every push to main. The URL never changes, because the tag never moves off that name.

```
weave-darwin-arm64    Apple Silicon
weave-darwin-amd64    Intel Mac
weave-linux-amd64
weave-linux-arm64
```

To verify a download, fetch `checksums.txt` from the same release and check against it.

```sh
shasum -a 256 -c checksums.txt --ignore-missing
```

## The database

Weave needs an empty database and a read-only login beside it. Export `WEAVE_DATABASE_URL`, or put it in `~/.config/weave/config.toml`:

```toml
[connection]
database_url = "postgres://weave:secret@localhost:5432/weave?sslmode=disable"
```

Weave migrates the schema on the way in, so an empty database is all you create. The built-in default is the checkout's docker-compose port, `localhost:5460`, so an installed binary needs this set.

```sh
weave readonly-role
```

That creates the least-privilege login weave's [MCP](mcp.md) query tools use, and the service will not start without it. It needs `CREATEROLE`, wants the schema to exist already, and is free to run again. Use [connection.readonly_database_url](configuration.md#connectionreadonly_database_url-weave_readonly_database_url) when it needs its own password.

## Upgrading

Download over the old binary. Config, database, and tmux config are untouched. The running processes keep the build they started as, so replace them too:

```sh
weave stop && weave start
tmux -L weave kill-server
```

Then `prefix + g` for a fresh panel. That kills the agents in it; their records and transcripts survive in the database.

## From a checkout

The contributor path, which needs Go 1.25.1 or newer.

```sh
git clone git@github.com:harrisoncramer/weave.git ~/weave
cd ~/weave
mise install
mise run go:build
mise run docker:up
```
