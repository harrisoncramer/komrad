# Installing weave

Weave runs coding agents in tmux panes on a tmux server of its own, and gives you one panel to watch them from. It all runs on your machine.

Weave is one statically linked binary. Installing it is a download, a `chmod`, and one line in your tmux config.

## Requirements

None of these are things a binary can bring with it.

A Mac or a Linux machine. Weave shells out to tmux for everything, so it is not containerised and there is no Windows build.

tmux 3.3 or newer. The panel is a `display-popup`, which arrived in 3.2, and the binding sets the popup title with `-T`, which arrived in 3.3. Check with `tmux -V`.

A POSIX shell as tmux's `default-shell`. Weave types a command line into a pane to start an agent, and fish and nushell parse that line differently, so the server refuses to start rather than leaving you with half-launched agents. `set -g default-shell /bin/sh` in your tmux config is enough, and it does not change the shell you get in a normal pane.

PostgreSQL 18 or newer, reachable from your machine. Eighteen is a floor rather than a preference: the schema defaults its primary keys to `uuidv7()`, which does not exist before 18. The database is yours, and weave migrates the schema inside it.

At least one agent CLI on your `PATH`, already logged in. Weave drives `claude` and `codex`. It does not manage their credentials.

## Install the binary

Every push to main publishes four binaries to one release, tagged `tip`. Pick the one for your machine:

```
weave-darwin-arm64    Apple Silicon
weave-darwin-amd64    Intel Mac
weave-linux-amd64
weave-linux-arm64
```

The URL never changes, because the tag never moves off the name `tip`:

```sh
curl -fsSL -o ~/.local/bin/weave \
  https://github.com/harrisoncramer/weave/releases/download/tip/weave-darwin-arm64
chmod +x ~/.local/bin/weave
```

Anywhere on your `PATH` works; `~/.local/bin` is a habit rather than a requirement. There is no archive to unpack and no second file to install.

To check the download arrived intact, fetch `checksums.txt` from the same release and verify against it. Save the binary under its release name for this, since that is the name the checksums use:

```sh
curl -fsSL -O https://github.com/harrisoncramer/weave/releases/download/tip/weave-darwin-arm64
curl -fsSL -O https://github.com/harrisoncramer/weave/releases/download/tip/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
mv weave-darwin-arm64 ~/.local/bin/weave && chmod +x ~/.local/bin/weave
```

Be clear about what that buys you. The checksums come from the same host as the binary, so they catch a corrupted transfer and not a compromised release.

What this channel is: the tip of main, rebuilt on every merge. It makes no version promise, there is no changelog, and a binary you download today may be a few commits ahead of one you downloaded this morning. `weave version` reports it as `v0.0.0-main.<shortsha>`.

### On a Mac

Use the `curl`, not the releases page in a browser. A binary fetched with `curl` is not quarantined and runs. A binary downloaded through a browser is quarantined by LaunchServices, and macOS then refuses it with `operation not permitted` and no dialog explaining why. If that happens:

```sh
xattr -d com.apple.quarantine ~/.local/bin/weave
```

The binaries are not signed with a Developer ID. The arm64 one is ad-hoc signed by the Go linker, which is what Apple Silicon requires to run a binary at all, and that is enough for the `curl` path.

### Private repository, for now

While the repository is private, a release asset needs an authenticated request, so the `curl` above will 404. With the [gh CLI](https://cli.github.com) logged in:

```sh
gh release download tip --repo harrisoncramer/weave --pattern weave-darwin-arm64 --output ~/.local/bin/weave
chmod +x ~/.local/bin/weave
```

## Bind the panel key

Weave opens in a tmux popup, and the binary binds the key itself. Add one line to your tmux config:

```tmux
run-shell 'weave tmux'
```

That is the whole integration. There is no tmux plugin to install. `weave` has to be findable when tmux parses the config, because the key is bound to the absolute path resolved at that moment, which is also why it keeps working afterwards on a tmux server whose `PATH` is minimal.

The binding is `prefix + g`. Press it to open the panel, press it again to hide it. Hiding is not quitting: the panel keeps running with its state intact, and so does every agent in it. Change the key with `set -g @weave_key 'w'` before the `run-shell` line.

A tmux server has to already be running for the bind to land, and the binding belongs to the server rather than to a session, so it survives every session you open and close and dies when the server does. Run `weave tmux` by hand to rebind after changing `@weave_key` without restarting tmux.

Inside the panel there is a second binding, `prefix + k`, which opens the command palette, and escape closes it. It lives on weave's own tmux server, so it cannot take a key away from your own sessions. Change it with `set -g @weave_palette_key 'x'`.

## Set up the database

Weave needs an empty Postgres database and a read-only login beside it. Put the connection string in `~/.config/weave/config.toml`:

```toml
[connection]
database_url = "postgres://weave:secret@localhost:5432/weave?sslmode=disable"
```

`WEAVE_DATABASE_URL` does the same job and beats the file. Weave migrates the schema on the way in, so an empty database is all you create.

The built-in default is `localhost:5460`, which is the port the repository's docker-compose publishes. That is right for a checkout and wrong for everything else, so an installed binary needs this step or its first run fails against a port nothing is on.

Then create the second login:

```sh
weave readonly-role
```

Weave connects twice. The second login is a least-privilege one that weave's [MCP](mcp.md) query tools use, and by default it is your URL with `weave_readonly` in place of the user. That separation is not decorative: the MCP surface hands a model a SQL prompt, and what a statement can read is decided by the role's privileges. Weave refuses to start if that login is a superuser, since a superuser is exempt from the privilege checks that make it safe. The command needs `CREATEROLE` on the writable login, wants the schema to exist already, and is free to run again. Do not skip it, because the service aborts startup when it cannot open that pool.

Set [connection.readonly_database_url](configuration.md#connectionreadonly_database_url-weave_readonly_database_url) when that login needs its own password.

## Check it

```sh
weave doctor
```

Doctor runs one check per dependency, prints a line each, and repairs nothing, because a diagnostic that changes what it is describing is not one you can trust. Failing lines carry the fix underneath them, and the command exits non-zero when something will stop the panel working. `weave doctor --json` prints the report if you would rather parse it.

Then, inside tmux, press `prefix + g`.

## Building from a checkout

This is the contributor path, and it needs Go 1.25.1 or newer.

```sh
git clone git@github.com:harrisoncramer/weave.git ~/weave
cd ~/weave
mise install
mise run go:build
```

That leaves the binary at `bin/weave`. [mise](https://mise.jdx.dev) pins the Go toolchain and every generator the repository uses, and carries the task tree; see [development](development.md) for the rest of the tasks.

A checkout can bring its own Postgres:

```sh
mise run docker:up
```

That starts Postgres 18 in Docker on port 5460, applies the migrations, and creates the read-only login, which is what the built-in connection default points at. A checkout with that database up needs no configuration at all.

Open the panel with `prefix + g`, or directly with `mise run panel`.

A source build reports a pseudo-version off the commit rather than the `main` stamp. That matters because the panel refuses to talk to a background service built from a different version and replaces it, so two checkouts on one machine keep restarting each other's service unless you give them separate addresses with `WEAVE_ADDR`.

## Upgrading

Download the binary again over the old one. Nothing else changes: your config, your database, and your tmux config all stay as they are.

The running processes do not notice, though, and that is worth knowing. The panel is a session on weave's own tmux server and the service is a separate long-lived process, and both keep running the build they started as. After replacing the binary:

```sh
weave stop && weave start
tmux -L weave kill-server
```

Then `prefix + g` for a fresh panel. That kills the agents running in it; their records and transcripts are in the database and survive.

## When it does not work

Run `weave doctor` first. Most of what goes wrong on a fresh install is one of its lines, and the line names the thing.

The service log is next. It is at `~/Library/Caches/weave/weave.log` on a Mac and `~/.cache/weave/weave.log` on Linux, one JSON record per line, so `jq` reads it and the panel's log key pages it.

If `prefix + g` does nothing at all, the key is not bound. `tmux list-keys -T prefix | grep weave` shows the binding if it is there. If it is missing, weave was not on `PATH` when tmux parsed its config; fix that and run `weave tmux` to bind it now.

A popup that flashes and vanishes takes its error with it. Run the command by hand to read it:

```sh
weave open
```

If `weave version` reports one build for this binary and another for the server, something is still running the old one. See [upgrading](#upgrading).

One thing that catches people who move their database: the connection settings are read once, when the panel session is created. Editing `connection.database_url` and pressing `prefix + g` does not reach a panel session that is already up. Restart weave's tmux server as above.

## Channels that do not exist yet

Homebrew is the one worth wanting, because a cask can depend on `tmux` and `postgresql@18` and no other channel installs anything but weave. There is no tap yet. Note for later that `weave` in homebrew/core is somebody else's tool, so it will need the tap prefix.

Tagged releases with real version numbers will sit alongside the `tip` channel rather than replacing it. `releases/latest` will point at the tag, `tip` will keep pointing at the tip of main, and neither instruction will change.

`go install` needs `go.mod` renamed from the bare `weave` to `github.com/harrisoncramer/weave`, after which it works with no release and no configuration.

From here, [configuration](configuration.md) covers the config files and the environment, [commands](commands.md) covers the CLI, and [how it works](concepts.md) covers the two tmux servers and why the panel can be closed without anything stopping.
