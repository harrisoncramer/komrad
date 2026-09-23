# Installing weave

## Quick start

```sh
curl -fsSL -o ~/.local/bin/weave \
  https://github.com/harrisoncramer/weave/releases/download/v0.1.0/weave-darwin-arm64
chmod +x ~/.local/bin/weave

echo "run-shell 'weave tmux'" >>~/.tmux.conf
tmux source-file ~/.tmux.conf

export WEAVE_OWNER_DATABASE_URL="postgres://weave:secret@localhost:5432/weave?sslmode=disable"
weave migrate
weave engine ensure laptop   # prints the database_url this engine connects with
export WEAVE_DATABASE_URL="<the url it printed>"
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

Four builds, on two channels.

```
weave-darwin-arm64    Apple Silicon
weave-darwin-amd64    Intel Mac
weave-linux-amd64
weave-linux-arm64
```

Stable is a numbered release with a changelog, cut deliberately.

```
https://github.com/harrisoncramer/weave/releases/download/v0.1.0/weave-darwin-arm64
```

Whereas `tip` is the tip of main, rebuilt on every merge. The URL never changes, and it makes no version promise. If you're running latest weave like this, expect breaking changes and bugs:

```
https://github.com/harrisoncramer/weave/releases/download/tip/weave-darwin-arm64
```

Both URLs answer 404 to an unauthenticated curl while this repository is private. Fetch them with `gh release download` until it is public.

To verify a download, fetch `checksums.txt` from the same release and check against it.

```sh
shasum -a 256 -c checksums.txt --ignore-missing
```

If you drive a remote engine, the two halves have to be the same build: the panel refuses an engine running other code. Install your weave from the release the engine is running, and move both together. [Running it remotely](remote.md) covers it.

## The database

Weave needs an empty database and a login that owns it. Several engines, each a weave on its own machine, can share one database, and each connects as a login of its own that Postgres maps to it, so no engine can read or write another's rows.

The owner migrates and hands out those logins. `weave migrate` brings the schema up to date, and `weave engine ensure NAME` creates an engine, its login and a read-only login for its MCP tools, and prints the url the engine connects with. Both connect as `owner_database_url`, which falls back to `database_url`:

```toml
[connection]
owner_database_url = "postgres://weave:secret@localhost:5432/weave?sslmode=disable"
database_url       = "postgres://weave_engine_laptop:<password>@localhost:5432/weave?sslmode=disable"
```

The service never migrates, and it refuses to start against a schema its build does not match or as a login that belongs to no engine. The built-in defaults are the checkout's docker-compose Postgres on `localhost:5460`, owned by `weave` and holding the `default` engine that `mise run docker:up` provisions, so an installed binary needs these set.

## Upgrading

Download over the old binary, naming the release you want. Config, database, and tmux config are untouched. Migrate as the owner, then replace the running processes, which keep the build they started as:

```sh
weave migrate
weave stop && weave start
tmux -L weave kill-server
```

Then `prefix + g` for a fresh panel. That kills the agents in it; their records and transcripts survive in the database. A panel that is open when the binary is replaced puts up a screen saying so; any key closes it, and the panel key opens one from the new build.

## From a checkout

The contributor path, which needs Go 1.25.1 or newer.

```sh
git clone git@github.com:harrisoncramer/weave.git ~/weave
cd ~/weave
mise install
mise run go:build
mise run docker:up
```
