# Installing weave

Weave runs coding agents in tmux panes on a tmux server of its own, and gives you one panel to watch them from. It all runs on your machine. There is nothing to sign up for.

Read this first: weave is not published anywhere yet. The repository is private, there are no tagged releases, there is no Homebrew tap, and the Go module is still named `weave` rather than a repository URL, so `go install` cannot resolve it. The only path that works today is building from a checkout. The rest of this page describes the channels weave is heading for, so you know what is coming and why each one leaves you a different amount of work. Every section that is not live says so at the top.

TODO TODO TODO: Write install file.

## Requirements

These are the same whichever way you install, because none of them are things a binary can bring with it.

A Mac or a Linux machine. Weave shells out to tmux for everything, so it is not containerised and there is no Windows build planned.

tmux 3.3 or newer. The panel is a `display-popup`, which arrived in 3.2, and the binding sets the popup title with `-T`, which arrived in 3.3. Check with `tmux -V`.

A POSIX shell as tmux's `default-shell`. Weave types a command line into a pane to start an agent, and fish and nushell parse that line differently, so the server refuses to start rather than leaving you with half-launched agents. Setting `set -g default-shell /bin/sh` in your tmux config is enough, and it does not change the shell you get in a normal pane.

PostgreSQL 18 or newer, reachable from your machine. Eighteen is a floor, not a preference: the schema defaults its primary keys to `uuidv7()`, which does not exist before 18. It also uses `pg_trgm` for transcript search. Weave is not going to manage a cluster for you; the database is yours, and weave migrates the schema inside it.

At least one agent CLI on your `PATH`, already logged in. Weave drives `claude` and `codex`. It does not manage their credentials, and it does not check whether they work before launching one, so install and sign in first.

Go 1.25.1 or newer, only if you are building from source.

## Building from a checkout

This is the only channel that works right now, and it is what the author runs.

```sh
git clone git@github.com:harrisoncramer/weave.git ~/weave
cd ~/weave
mise install
mise run go:build
```

That leaves the binary at `bin/weave`. [mise](https://mise.jdx.dev) is doing two things worth knowing about: it pins the Go toolchain and every generator the repository uses, and it carries the task tree. See [development](development.md) for the rest of them.

If you do not have a Postgres already, the checkout ships one:

```sh
mise run docker:up
```

That starts Postgres 18 in Docker on port 5460, applies the migrations, and creates the read-only login. Port 5460 is not an accident: it is what the built-in `connection.database_url` default points at, so a checkout with the compose database up needs no environment at all. That default is wrong for every other channel, which is called out below.

Then either press `prefix + g` if you have the tmux plugin wired up, or open the panel directly:

```sh
mise run panel
```

A source build reports itself as a pseudo-version off the commit. That matters more than it sounds, because the panel refuses to talk to a background service built from a different version and replaces it. Two checkouts on one machine will keep restarting each other's service unless you give them separate addresses with `WEAVE_ADDR`.

## Homebrew

Not live. There is no tap yet.

Homebrew is meant to be the primary channel, and the reason is that it is the only one that installs weave's dependencies. A cask can depend on `tmux` and on `postgresql@18`, on macOS and on Linux both, so the install that puts weave on your machine also puts the two things weave cannot run without on it. Nothing else here does that.

It will look like this:

```sh
brew trust harrisoncramer/tap
brew install harrisoncramer/tap/weave
```

Two details that will not be optional. The trust line comes first because Homebrew 6.0 made third-party taps untrusted by default, and while naming the tap on the install command counts as consent, a later `brew upgrade` does not name anything and quietly skips untrusted taps with a warning nobody reads. Trusting it once means upgrades keep arriving. And the tap prefix is load-bearing: `weave` in homebrew/core is somebody else's tree-sitter merge driver, so `brew install weave` gets you that instead.

What brew leaves you with: a binary, tmux, and a Postgres that is installed but not started and has no weave database in it. See the setup steps below.

## The tmux plugin

The plugin is not really an install channel, it is the keybinding, and you want it whichever channel you came through. Weave is opened from a tmux popup, and `weave.tmux` is what binds the key.

With [tpm](https://github.com/tmux-plugins/tpm), add this to `~/.tmux.conf` and press `prefix + I`:

```tmux
set -g @plugin 'harrisoncramer/weave'
```

Without tpm, source the script directly:

```tmux
run-shell /path/to/weave/weave.tmux
```

The binding is `prefix + g`. Press it to open the panel, press it again to hide it. Hiding is not quitting: the panel keeps running with its state intact, and so does every agent in it. Rebind with `set -g @weave_key 'w'` before the plugin line.

Inside the panel there is a second binding, `prefix + a`, which opens the [action](actions.md) palette over the pane you press it in. It is bound on weave's own tmux server rather than yours, so it exists only inside the popup and cannot take a key away from your own sessions. Rebind it with `set -g @weave_palette_key 'x'`.

One honest warning about the current script. It hardcodes the binary to `bin/weave` inside the plugin directory and builds it with `go build ... || true` when it is missing, which means a machine with no Go toolchain, or a checkout that does not compile, gets a silent no-op and a key bound to a path with nothing at it. Pressing the key then opens a popup that fails. If `prefix + g` does nothing useful, that is the first thing to check. The intended fix is for the plugin to search for a `weave` on `PATH` and in the usual install locations, and to bind the key to a visible complaint when it cannot find one.

## A prebuilt binary from a release

Not live. There are no tags and no release assets.

The plan is GoReleaser on a pushed tag, producing tarballs for darwin and linux on amd64 and arm64, plus a `checksums.txt` covering all four. Four platforms and no more, since weave drives tmux and Postgres and nobody runs a fleet of coding agents on a 386.

When it exists, the manual version is the usual shape:

```sh
curl -fsSLO https://github.com/harrisoncramer/weave/releases/latest/download/weave_0.1.0_darwin_arm64.tar.gz
curl -fsSLO https://github.com/harrisoncramer/weave/releases/latest/download/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
tar -xzf weave_0.1.0_darwin_arm64.tar.gz
mv weave ~/.local/bin/weave
```

There will also be a small install script that does exactly that, for Linux machines without Homebrew. It will install to `~/.local/bin` and never ask for sudo, and it will refuse to write anything on a checksum mismatch. Worth being clear about what that buys you: the checksums come from the same host as the archive, so they defend against a corrupted download and not against a compromised release. Weave spawns agents with tool access and holds a database login, so read the script before you run it rather than piping it into a shell.

The tarball will carry `weave.tmux` alongside the binary, so you can source it directly if you are not using tpm.

## go install

Not live. `go.mod` says `module weave`, a bare name with no domain, so there is nothing for the toolchain to resolve. Once the module is renamed to `github.com/harrisoncramer/weave` this works with no release and no configuration on weave's side:

```sh
go install github.com/harrisoncramer/weave@latest
```

This channel is for contributors and for people who want a specific commit rather than a release. It gives you a binary in `$GOBIN` and nothing else: no tmux plugin, no keybinding, no Postgres, no database. The migrations travel with the binary, so `weave` can still bring a schema up on its own, but everything in the next section is on you.

## After installing, whichever way you did it

Only the checkout with `mise run docker:up` skips all of this. Everything else lands here.

### Point weave at a database

One connection string, in your config file:

```toml
[connection]
database_url = "postgres://weave:secret@localhost:5432/weave?sslmode=disable"
```

That goes in `~/.config/weave/config.toml`, which is the file weave reads. An environment variable does the same job and beats the file, so `export WEAVE_DATABASE_URL=...` still works and is the better choice for one shell rather than for good.

Weave migrates the schema through it on the way in, so an empty database is all you have to create.

Weave connects twice, though. The second login is a least-privilege one that weave's [MCP](mcp.md) query tools use, and by default it is that URL with `weave_readonly` in place of your login, on the same server and the same database. That separation is not decorative: the MCP surface hands a model a SQL prompt, and what a statement is allowed to read is decided by the role's privileges rather than by anything weave can inspect. Weave refuses to start if that login is a superuser, because a superuser is exempt from the privilege checks on the catalogs and on the file-reading functions and both are reachable from a plain `SELECT`. Set [connection.readonly_database_url](configuration.md#connectionreadonly_database_url-weave_readonly_database_url) when it needs its own password or reaches the database another way.

The built-in default points at `localhost:5460`, which is the port the repository's docker-compose publishes. That is right for a checkout and wrong for a brew-installed Postgres, which listens on 5432, so if you skip this step on any other channel your first run fails against a port nothing is on.

### Create the read-only login

```sh
weave readonly-role
```

This connects as whichever database URL is in force, creates the read-only role, and grants it read on the application tables and nothing else. It needs `CREATEROLE` on the writable login. Run it after the schema exists, so the grant covers the tables that are there; it also sets default privileges, so tables added by later migrations are covered too. Running it again is free.

Do not skip it. The service opens that second pool during startup and aborts if it cannot, so an install that got everything else right and missed this step gets a panel that will not open.

### Add the tmux plugin line

See the plugin section above. This is the one step weave will not do for you, because `~/.tmux.conf` is a file you wrote.

### Start the service

```sh
weave start
```

The first `prefix + g` does this for you, but running it by hand means a failure is printed to a terminal that can show several lines rather than to a popup that shows one. The service is long-lived: it holds the Postgres connections, serves the API and the MCP endpoint, and runs the loops that watch every pane. Closing the panel does not stop it, and neither does closing the terminal.

### Check the setup

```sh
weave doctor
```

Doctor runs one check per dependency, prints one line each, and repairs nothing, because a diagnostic that changes what it is describing is not one you can run to find out what is wrong. A healthy machine looks like this:

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

Failing lines carry the fix underneath them, and the command exits non-zero when something will stop the panel working, so it scripts. `weave doctor --json` prints the raw report if you would rather parse it.

Then, inside tmux, press `prefix + g`.

## When it does not work

Run `weave doctor` first. Most of what goes wrong on a fresh install is one of its lines, and the line names the thing rather than making you infer it from a stack trace.

The service log is the next place. It is at `~/Library/Caches/weave/weave.log` on a Mac and `~/.cache/weave/weave.log` on Linux, one JSON record per line, so `jq` reads it and the panel's log key pages it. Anything the service failed to do on startup is explained there, and it is the right place to look when the panel is showing something that makes no sense.

A popup that flashes and vanishes takes its error message with it. Run the same command by hand to see what it said:

```sh
weave open
```

If `weave version` reports one build for this binary and a different one for the server, you have upgraded and something is still running the old one. `weave stop` and then `weave start` sorts the service out. If the panel itself is stale, it is a session on weave's own tmux server, so `tmux -L weave kill-server` and another `prefix + g` gets you a fresh one. Your agents live on that server too, so that kills them; their records and transcripts survive in the database.

One thing that catches people who change where weave connects: the connection settings are read once, when the panel session is created, and reused after that. Editing `connection.database_url` or exporting a new `WEAVE_DATABASE_URL` and pressing `prefix + g` will not reach a panel session that is already running. Restart the tmux server as above.

From here, [configuration](configuration.md) covers the config files, layering them, and the environment, [commands](commands.md) covers the CLI, and [how it works](concepts.md) covers the two tmux servers and why the panel can be closed without anything stopping.
