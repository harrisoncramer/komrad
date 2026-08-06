# Installing weave

Weave runs coding agents in tmux panes on a tmux server of its own, and gives you one panel to watch them from. It all runs on your machine.

Weave is one statically linked binary. Installing it is a download, a `chmod`, and one line in your tmux config.

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

`weave version` reports them as `v0.0.0-main.<shortsha>`. This channel is the tip of main, with no version promise and no changelog.

To verify a download, fetch `checksums.txt` from the same release and check against it. Same host as the binary, so it catches a corrupted transfer and not a compromised release.

```sh
shasum -a 256 -c checksums.txt --ignore-missing
```

On a Mac, use `curl` rather than the releases page. A browser download is quarantined and dies with `operation not permitted` and no dialog. Undo it with `xattr -d com.apple.quarantine ~/.local/bin/weave`.

## The panel key

The binary binds the key itself, so there is no tmux plugin to install:

```tmux
run-shell 'weave tmux'
```

`weave` has to be on `PATH` when tmux parses the config, since the key is bound to the absolute path resolved at that moment.

`prefix + g` opens the panel and hides it again. Hiding is not quitting: the panel and every agent in it keep running. Change it with `set -g @weave_key 'w'` above the `run-shell` line, then run `weave tmux` to rebind without restarting tmux. Inside the panel, `prefix + k` opens the command palette; change that with `set -g @weave_palette_key 'x'`.

To use the panel in a plain terminal rather than the popup, `weave attach` shows the running one there, and the panel key detaches it. It attaches a panel that is up rather than starting one.

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

## When it does not work

`weave doctor` is one line per dependency, with the fix under any that fail. It exits non-zero when something will stop the panel working, and `--json` prints the report instead. After that, the service log at `~/Library/Caches/weave/weave.log`, or `~/.cache/weave/weave.log` on Linux.

If `prefix + g` does nothing, the key is not bound. `tmux list-keys -T prefix | grep weave` shows it if it is there; if not, weave was not on `PATH` when tmux parsed the config, so fix that and run `weave tmux`.

A popup that flashes and vanishes takes its error with it. Run `weave open` by hand to read it.

If `weave version` disagrees with itself about the binary and the server, see [upgrading](#upgrading).

Connection settings are read once, when the panel session is created. Editing `database_url` and pressing `prefix + g` does not reach a panel that is already up; restart weave's tmux server as above.

## From a checkout

The contributor path, which needs Go 1.25.1 or newer.

```sh
git clone git@github.com:harrisoncramer/weave.git ~/weave
cd ~/weave
mise install
mise run go:build
mise run docker:up
```

That leaves the binary at `bin/weave` and a Postgres 18 on port 5460 with migrations and the read-only login applied, which is where the built-in default points, so a checkout needs no configuration. Open the panel with `prefix + g` or `mise run panel`. See [development](development.md) for the rest of the tasks.

A source build reports a pseudo-version off the commit rather than the `main` stamp, and the panel replaces any service built from a different version, so two checkouts on one machine need separate addresses with `WEAVE_ADDR`.

From here, [configuration](configuration.md) covers the config files and the environment, [commands](commands.md) covers the CLI, and [how it works](concepts.md) covers the two tmux servers.
