# Installing weave

## Quick start

```sh
curl -fsSL -o ~/.local/bin/weave \
  https://github.com/harrisoncramer/weave/releases/download/v0.1.0/weave-darwin-arm64
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

Weave needs an empty database and a read-only login beside it. Export `WEAVE_DATABASE_URL`, or put it in `~/.config/weave/config.toml`:

```toml
[connection]
database_url = "postgres://weave:secret@localhost:5432/weave?sslmode=disable"
```

Weave migrates the schema on the way in, so an empty database is all you create. The built-in default is the checkout's docker-compose port, `localhost:5460`, so an installed binary needs this set.

## Upgrading

Download over the old binary, naming the release you want. Config, database, and tmux config are untouched. The running processes keep the build they started as, so replace them too:

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

The panel key runs whichever binary bound it, so `mise run go:build` and `mise run restart` end by binding the key again from the build they just made, for the local panel and for every engine under `~/.config/weave/engines`. A panel that is running when its binary changes closes itself on its next refresh, and the key opens one from the new build.
