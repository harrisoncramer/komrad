# Commands

The `weave` binary includes the service that watches your agents and the panel you watch them from.

Most days you type none of this, because the tmux manager makes it unnecssary. These commands may be helpful, though, if you are just setting a machine up, for scripts, or when the UI is buggy.

## serve

The service. It opens Postgres, mounts the API and the [MCP](mcp.md) endpoint on one address, and runs the loops that read each pane, fire [schedules](schedules.md), and sync transcripts. Nothing in weave works without it.

Run it here when you want to watch its output. Otherwise let `weave start` or the panel spawn it.

```bash
weave serve --addr 127.0.0.1:8787
```

## start

Starts the service in the background if it isn't already up. It asks the address who's there first: a server of this build is left alone, and one of another build is asked to stop and replaced, so a rebuilt binary is the one answering.

Opening the panel does this on its own, so you mostly want it in a login script or a CI setup.

```bash
weave start
```

## stop

Asks the running service to stop and waits until the process is actually gone. It asks rather than signals, so a launch in flight gets to finish.

Your agents keep running. They're tmux panes, not children of the service, and they get picked back up when it returns.

```bash
weave stop
```

## doctor

Checks that this machine has what weave needs, one line per dependency: tmux and its version, the shell tmux starts panes with, whether your [config](configuration.md) parses, the database and its schema, the read-only login the MCP query tools use, the service, and the CLI behind each [agent](agents.md) harness.

It repairs nothing, and exits non-zero when a check failed, so it can gate a script. Run it first when the panel is doing something that makes no sense.

```bash
weave doctor
weave doctor --json
```

## migrate

Applies pending migrations to the configured database. The service migrates on start, so this is for migrating without starting one: a fresh database, or a deploy step. It's safe to run against a database that's already current.

```bash
weave migrate
```

## version

Prints which build this binary is and which build is answering on the address. The two differ more often than you'd think, because the service keeps the binary it started with and rebuilding only replaces the file on disk. When the second line isn't the first, restart.

```bash
weave version
```

## rpc

One subcommand per RPC, named the way the [MCP](mcp.md) tools name them, so a script and a model ask for the same thing by the same name. Request fields are flags and the response is the API's JSON on stdout.

```bash
weave rpc list-agents --compact
```

## cookbook

Installs a [cookbook](cookbooks.md): a workflow, its actions, and an optional schedule, packaged as a directory and installed by name over the same endpoints the panel and the MCP tools already call.

```bash
weave cookbook list
weave cookbook install worktree-agent --cwd ~/myrepo
```

## readonly-role

Creates the least-privilege Postgres role the MCP query tools connect as, so the SQL a model writes is fenced by the database rather than by inspecting the query. Run it once when setting a database up. It refuses if your two database URLs name the same login.

```bash
weave readonly-role
```

## The full reference

Every command's usage line, flags, and long description live in [reference/](reference/weave.md), generated from the command tree itself. That's the place to look for exact flag names and defaults, including the `rpc` subcommands, which are too many to list here.
