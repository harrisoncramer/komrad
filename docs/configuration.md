# Configuration

Everything weave has is a key in a TOML file. Which agent CLI to run, per-CLI flags, keybindings, marks, layout, the log level, and also the database it connects to, the address it serves on, the tmux socket it runs its own server on, and where it keeps its cache.

You can also set the last few from the environment, and the environment wins. That is what makes a second instance possible: a shell with `DATABASE_URL` and `WEAVE_TMUX_SOCKET` exported gets its own database and its own tmux server without touching the file your usual weave reads.

And the file can be several files, layered in order. That is for keeping a base you share or check in, with the machine-specific parts in a file beside it that never leaves the machine.

So the whole of it, weakest first: the built-in default, then each file in the order it was given, then the environment.

## The config file

Weave reads two files by default, and neither has to exist:

```
~/.config/weave/config.toml
~/.config/weave/config.local.toml
```

The second is read after the first, so it wins where they overlap. Names apart there is nothing special about either one. They are just the two layers weave looks for when you have not told it otherwise. With no file at all every default applies and the panel works fine, which is the ordinary install.

Write them yourself and put only the settings you are changing in them. Anything you leave out falls back to its built-in default. There is no init command; `touch ~/.config/weave/config.toml` and start typing.

A key weave does not recognise is an error, not a line quietly ignored. That is deliberate: a typo that does nothing is a setting you will spend twenty minutes deciding is broken. The error names the file, the key, and what that key's table actually takes:

```
failed to parse config:
  /Users/you/.config/weave/config.toml
    max_agent is not a setting; the top level takes default_harness, max_agents,
    harness, layout, marks, keybindings, actions, context, log
  /Users/you/.config/weave/config.local.toml
    log.levl is not a setting; [log] takes level, viewer
```

Every layer that has a bad key is named in one go, rather than stopping at the first, so you fix them in one pass instead of rerunning and being told about the next one.

`weave doctor` prints which files are being read, in order, which of them exist, and where each of the connection settings ended up coming from. Run that first when a setting is not taking effect.

### What happens when it will not parse

Most commands refuse to run. That includes `weave serve`, `weave open` and `weave start`, so nothing connects to a database on a config it could not read. This matters more than it used to: the file can name your database now, and falling back to built-in defaults would quietly point weave at a different one, possibly an old local Postgres, and write there.

The panel is the exception, and shows an error screen naming the file and the key instead of exiting, because a panel that exits takes its own error message with it when tmux closes the popup. Nothing about your agents has changed and nothing has connected anywhere. Fix the key and press `prefix + g` again.

`weave doctor` and `weave version` also still run, since a broken config is the thing you want to ask doctor about.

### Layering more than two files

`--config` names a layer, and you can pass it as many times as you like. Order is the order you write, last wins:

```sh
weave --config /etc/weave/base.toml \
      --config ./weave.toml \
      --config ~/.config/weave/config.local.toml \
      open
```

Passing `--config` at all replaces the default discovery entirely. So the two files in `~/.config/weave` are not read unless you name them, and no `.local.toml` sibling is picked up for a path you passed. If you want your own file layered on top of your usual ones, say so:

```sh
weave --config ~/.config/weave/config.toml --config ./experiment.toml open
```

This is the same rule `docker compose -f` follows, and for the same reason: a flag that sometimes adds to a hidden list and sometimes replaces it is a flag you have to remember.

The layer list is forwarded to the panel session and to the background service it spawns, so all three read the same files in the same order.

A file you named with `--config` and is not there is an error, naming which one. A file weave discovered on its own and did not find is not, because not having written one is the ordinary install.

`WEAVE_CONFIG` does the same thing for a shell that cannot pass flags, holding several paths separated by colons the way `PATH` does:

```sh
export WEAVE_CONFIG="$HOME/.config/weave/config.toml:$PWD/weave.toml"
```

Flag beats variable beats discovery, and exactly one of the three is in force. `--config` does not append to `WEAVE_CONFIG`, it replaces it, so a checkout can drop a layer your shell exported rather than being stuck with it.

### How layers combine

A plain value is replaced by the last layer that sets it.

A table is merged key by key, so a later layer changing `log.level` leaves `log.viewer` alone.

A list is replaced whole by the last layer that sets it, and each list is its own unit. A layer that sets `marks` replaces the marks and does not touch your keybindings. A layer that sets `keybindings.agents.new` replaces that one binding and leaves the other twelve alone.

Lists are not appended to. There would be no way to remove an entry a lower layer put there, and for `marks` the order is the order the mark key cycles through, which is yours to decide rather than something a base file gets the first say in.

There is no way to say "no marks at all". An empty list reads as unset and falls back to the built-in three.

### About config.toml in the repo

The `config.toml` at the root of the weave repository is generated. It is rendered from the `Config` struct in `internal/appconfig`, taking each key's description from the Go doc comment above it, by `mise run gen:config`.

It is a reference, not a template. Do not copy it into `~/.config/weave/`. Three reasons now. Editing the repo file changes nothing, because weave never reads it, and the next `mise run gen:config` overwrites your edits. Copying all of it leaves you holding every key weave has, so a rename in a later version breaks your config even for settings you never touched. And it spells out `database_url` at its built-in default, which is the local Docker Postgres from this repo, so copying the file wholesale is a way to point weave at a database you did not mean.

Read it, take the two lines you want, leave the rest.

## Top level

### default_harness

Which agent CLI a new agent runs when neither the agent nor its [workflow](workflows.md) names one. Default `"claude"`. It has to be a harness this build implements, which is one of the names under `[harness]` below, so `"claude"` or `"codex"`.

Change it if you mostly use Codex. Getting it wrong is caught at startup rather than at the first create, and `weave doctor` names the ones your build has.

### max_agents

How many unarchived agents may exist at once. Default `50`. Past this a create is refused.

The number is not about your machine, it is a backstop against a caller in a loop. If you drive weave from an orchestrator through the [MCP layer](mcp.md), a bug in the orchestrator can otherwise spawn panes until tmux gives up. Raise it if you legitimately run more; lower it if you want the loop caught sooner.

### marks

The marks the mark key cycles through, in order. Press the key on an agent and it steps to the next mark, and past the last one back to unmarked. Nothing but you ever sets a mark, so they mean whatever you decide.

```toml
marks = [
  { glyph = "●", color = "yellow" },
  { glyph = "◐", color = "yellow" },
  { glyph = "✓", color = "yellow" },
]
```

A colour is either one of the sixteen your terminal's own scheme maps, named as `"red"` or `"brightred"`, or a hex value like `"#DCA561"`. Prefer a name. The panel draws itself out of those sixteen, so a named mark stays in the same scheme as everything around it, while hex is the same in every terminal whether that terminal's scheme agrees or not.

The glyph is what a row draws and what gets stored, so it is the mark's identity. Weave refuses a glyph that is empty, longer than sixteen bytes, carries whitespace or control characters, or is listed twice.

A useful set is one per thing you are waiting on:

```toml
marks = [
  { glyph = "!", color = "red" },
  { glyph = "?", color = "yellow" },
  { glyph = "✓", color = "green" },
]
```

## harness

Per-CLI settings. There is a table per agent CLI weave implements, and a table for one it does not is an error, so this section doubles as the list of what can be run.

### harness.claude.flags

Flags appended to every Claude agent's command line, ahead of whatever flags that agent carries itself. Default empty.

```toml
[harness.claude]
flags = "--permission-mode acceptEdits"
```

This is the place for the flag you would otherwise retype into every create. Flags are split into arguments and quoted before they reach the shell, so they are data rather than shell text; you cannot smuggle a pipeline through here.

### harness.codex.flags

The same thing for Codex.

```toml
[harness.codex]
flags = "-m gpt-5.6 -s read-only"
```

### harness.codex.trust_launch_dirs

Whether to record each agent's directory as trusted in Codex's own config before launching it. Default `false`.

Codex asks whether a directory is trusted the first time it runs there, and waits for an answer it will only read from its own config file. So an agent in a fresh worktree starts blocked on that question and shows up in the panel as waiting for a human, which is not what you wanted from a background agent.

Turn it on if you run Codex agents in new worktrees, which is most of the point of [worktrees](agents.md). Know what it does first: it edits `~/.codex/config.toml` on your behalf, adding each directory weave launches into.

```toml
[harness.codex]
trust_launch_dirs = true
```

## layout

How the popup is divided.

`split` is the share of the width the panel's list takes, default `0.2`. The rest is the stage beside it, which is where the agent you are hovering is drawn.

`secondary_split` is the share of the stage's height taken by the pane that opens under it, default `0.4`. That pane is where the log, the shell, and the agent's record appear.

Both are fractions above zero and below one. Anything else is refused at load rather than handed to tmux.

```toml
[layout]
split = 0.3
secondary_split = 0.5
```

Widen the split if your agent names are long or you keep a lot of workflows open. Narrow it if you mostly want to read the agent.

## keybindings

Bindings are scoped to the screen they act in, so the same key can mean different things in the agents list and in the archive. Each binding is a list of key strings such as `"enter"`, `"ctrl+d"`, `"esc"`, or `" "` for space.

Every default is a single key, which leaves you the alternatives. Setting a binding replaces its default rather than adding to it, so list both if you want both:

```toml
[keybindings.agents]
new = ["n", "c"]
```

A binding you do not set keeps its built-in default, and the fallback is per binding rather than per table. Writing one key under `[keybindings.agents]` does not blank the other twelve.

The groups are `actions`, `agents`, `archive`, `filter`, `form`, `help`, `logs`, `nav`, `schedules`, `selection`, and `workflows`. Three of them are shared rather than belonging to a screen: `nav` is movement everywhere, `help` is the legend, `logs` opens the service log, and `filter` narrows whichever list is on screen. Movement being configured once is on purpose, since a panel where `j` moves down on one screen and not another is worse than one you cannot rebind.

`?` in the panel draws the legend for the screen you are on, which is the honest list of what is bound right now. The generated `config.toml` in the repo spells out every group and every default if you would rather read them all at once.

Two things are not configurable. The keys that close the filter input are fixed, because while it is open every other key types into the pattern. And the form's submit is `ctrl+s` rather than enter, because plain enter belongs to the text inputs.

## actions

### actions.dir

Where the panel finds the scripts its action key runs. Default `~/.config/weave/actions`.

One executable file per action, named after the action. They run in the agent's directory with the agent as JSON in `WEAVE_AGENT`. A file here overrides a built-in action of the same name, which is how you replace `_terminal` with your own shell. See [actions](actions.md).

```toml
[actions]
dir = "~/dotfiles/weave/actions"
```

A leading `~` is expanded. Set this if you keep your scripts in a dotfiles repo rather than in the config directory.

## context

### context.file

The file holding standing text every agent is started with, whatever workflow it is in. Default `~/.config/weave/context.md`.

This is where facts about your machine and how you want to be worked with go, so they do not have to be pasted into every workflow. It is read at each launch, so editing it changes what the next agent is told without restarting anything.

The file not existing is not an error. Leaving this key empty means the default path, not no context.

```toml
[context]
file = "~/dotfiles/weave/context.md"
```

## log

### log.level

How much the background service writes about itself. One of `debug`, `info`, `warn`, `error`. Default `"info"`. A level weave does not know is refused at load, because an unchecked typo would otherwise decode as debug and turn the log up rather than telling you the word was wrong.

Turn it to `debug` when you are working out why an agent did not launch, and put it back.

### log.viewer

The command the panel's log key runs. Default:

```toml
[log]
viewer = 'less -R +F "$WEAVE_LOG"'
```

The path to the log is exported as `WEAVE_LOG` rather than substituted into your command, so you can write anything a shell can. That matters for the tools people actually want here: fzf's own `{}` means the selected line, and a panel that rewrote `{}` before fzf saw it would make half the recipes in fzf's manual impossible.

The log is one JSON record per line, so:

```toml
[log]
viewer = 'jq -c . "$WEAVE_LOG" | fzf --ansi --tac'
```

Where the log is written is not configurable. The process that spawns the service points its output at the file and the log key pages the same file, and a path those two could disagree about is a log nobody can find. It is `weave.log` under the cache directory, so `~/Library/Caches/weave/weave.log` on a Mac and `~/.cache/weave/weave.log` on Linux.

## Connection and instance settings

These six decide which weave you are talking to. Each has a key in the file and an environment variable, and the variable wins when it is set to something non-empty.

Both exist because they answer different questions. The file is where you write down the database you always use, so it survives a new shell and is not buried in a profile. The variable is how you point one shell, or one checkout, somewhere else without editing anything.

The key and the variable for each:

```
database_url           DATABASE_URL
readonly_database_url  WEAVE_READONLY_DATABASE_URL
addr                   WEAVE_ADDR
tmux_socket            WEAVE_TMUX_SOCKET
cache_dir              WEAVE_CACHE_DIR
editor                 EDITOR
```

All six are top-level keys:

```toml
database_url = "postgres://weave:secret@db.internal:5432/weave?sslmode=require"
addr = "127.0.0.1:8790"
tmux_socket = "weave-work"
```

An empty variable counts as unset, so `DATABASE_URL=` falls through to the file rather than blanking it.

Passwords in a file are worth a thought. `~/.config/weave` is yours, but the two connection strings are the one part of your config that is a secret, so keep them in the layer you do not share. If you check a base file into a repository, put the local layer in `.gitignore`. Weave redacts both down to login and host anywhere it hands your config back, so `weave rpc get-config` and the [MCP](mcp.md) `get_config` tool show `postgres://weave@db.internal:5432/weave` and never the password.

### database_url, DATABASE_URL

The Postgres weave stores everything in: agents, workflows, schedules, transcripts. Defaults to `postgres://weave:weave@localhost:5460/weave?sslmode=disable`, which is what `mise run docker:up` starts in this repo.

Weave brings the schema up to date itself when the service starts. There is no migration step to run by hand.

### readonly_database_url, WEAVE_READONLY_DATABASE_URL

A second login into the same database, for reading only.

Set nothing and weave derives it from whichever database URL is in force, keeping the host, the port, the database, and the options exactly as you wrote them and putting the login `weave_readonly` in place of yours. The derivation happens after the file and the environment have been resolved, so a file that sets only `database_url` gets a read-only login on that database rather than on the built-in default. Moving the database moves both halves, and the two can only land on different databases if you say so here.

Weave needs two logins rather than one because the [MCP](mcp.md) surface lets a model run SQL. The connection is opened read-only, but read-only stops writes and only writes; what a `SELECT` is allowed to see is decided by the role's privileges. So the query tools connect as a role that has read on the application tables and nothing else.

Weave refuses to start if that URL lands on a superuser, and says so rather than warning, because a superuser is exempt from the privilege checks on the catalogs and on the file-reading functions, and both are reachable through a plain `SELECT` that writes nothing.

Set it when the read-only login needs a password of its own, or reaches the database by another route. In a file:

```toml
database_url = "postgres://weave:secret@db.internal:5432/weave?sslmode=require"
readonly_database_url = "postgres://weave_readonly:other@db.internal:5432/weave?sslmode=require"
```

Or for one shell:

```sh
export DATABASE_URL='postgres://weave:secret@db.internal:5432/weave?sslmode=require'
export WEAVE_READONLY_DATABASE_URL='postgres://weave_readonly:other@db.internal:5432/weave?sslmode=require'
```

Either way, run `weave readonly-role` once afterwards.

`weave readonly-role` creates the role named in the read-only URL, with the password in it, and grants it read on the application tables. It connects as `DATABASE_URL` and needs `CREATEROLE`. Run it once, and again after an upgrade that adds a table. It refuses to run if the two URLs name the same login, since that would be a read-only role with your write privileges.

`weave doctor` reports a pair naming two different databases, since both halves connect and nothing else would say the panel and the query tools are reading different rows. It reports rather than refuses, because an address weave cannot match up may still be the same database behind a connection pooler or a second name for the host.

### addr, WEAVE_ADDR

Where the background service listens. Default `127.0.0.1:8787`. The panel, the CLI, and the MCP endpoint all talk to this address.

Change it when you run two weaves on one machine. Two instances on the same address will keep replacing each other's server, because the panel refuses to talk to a server built from a different version and restarts it.

`weave serve --addr` overrides it for a service started by hand, above both the file and the variable.

### tmux_socket, WEAVE_TMUX_SOCKET

The tmux socket weave runs its own server on. Default `weave`.

Weave does not use your tmux server. It starts a second one and puts the manager session and every agent pane on it, which is why nothing in your normal tmux sees any of this. To look at it directly:

```sh
tmux -L weave ls
```

Change this along with `addr` and `database_url` when you want a second, isolated instance. Those three together are what isolation means here, and a layer holding all three is the tidiest way to keep one:

```toml
database_url = "postgres://weave:weave@localhost:5461/weave?sslmode=disable"
addr = "127.0.0.1:8790"
tmux_socket = "weave-test"
cache_dir = "~/Library/Caches/weave-test"
```

### cache_dir, WEAVE_CACHE_DIR

Where weave keeps what it generates: the service log, the built-in action scripts it writes out on every start, and the temp files a harness passes a prompt through. Defaults to `~/Library/Caches/weave` on a Mac and `~/.cache/weave` on Linux.

Everything under it is weave's to delete and rewrite, so nothing of yours belongs here. Set it if your cache directory is not writable, or to keep a second instance's log out of the first one's.

### editor, EDITOR

The editor the panel's editor key opens an agent's directory in. Default `vi`.

Most people already export `EDITOR` and never need the key. It exists so that "everything is in the file" is true without an exception you have to remember, and because the editor you want for a weave pane is not always the one you want everywhere.

### Set by tmux and by weave

`TMUX` and `TMUX_PANE` are set by tmux, and weave reads them to work out which of your clients opened the popup. `WEAVE_AGENT_ID` and `WEAVE_SESSION_ID` are set by weave into every pane it launches, so a call an agent makes back through the MCP endpoint is attributed to that agent rather than to nobody. Do not set any of these yourself.

## When a change takes effect

Most settings take effect when you reopen the panel. The files are read at startup by the panel and by the service, so changing a keybinding or a harness flag needs nothing killed.

The six connection settings are the exception, whether you set them in a file or in the environment. They are read once, when the panel session is created, and reused from then on.

The reason is tmux. A tmux server takes its environment once, from whatever shell started it, and hands that to every session created afterwards. Weave's server is long-lived, so that environment could be weeks old. Weave works around it by resolving all six itself and forwarding the resolved values into the manager session with `-e`, which pins them to the shell that opened the popup. Only the open that creates the session does that. Every later one finds it already up.

So after changing any of the six, restart:

```sh
weave stop
tmux -L weave kill-server
```

Then press `prefix + g` again. This kills the tmux server weave runs its agents on, so anything mid-turn loses its pane. The records and transcripts are in Postgres and survive.

One consequence worth knowing: because the resolved values are forwarded into the tmux server's environment, `tmux -L weave show-environment` will show them, database password included, and every pane inherits them. That is the cost of pinning them, and it is why the panel does not re-read them per pane.

## A minimal config

Most people's config is three or four lines. This one runs Codex by default and accepts edits without asking:

```toml
default_harness = "codex"

[harness.codex]
trust_launch_dirs = true

[harness.claude]
flags = "--permission-mode acceptEdits"
```

## A fuller one

Everything below is optional and every key shown is at something other than its default, so this is a config that actually changes things rather than a copy of the built-ins.

```toml
# claude or codex, for agents whose workflow names neither
default_harness = "claude"

# refuse a create past this many live agents, in case an orchestrator loops
max_agents = 25

# what the mark key cycles through: broken, waiting on me, done
marks = [
  { glyph = "!", color = "red" },
  { glyph = "?", color = "yellow" },
  { glyph = "✓", color = "green" },
]

[harness.claude]
flags = "--permission-mode acceptEdits"

[harness.codex]
flags = "-s workspace-write"
# answer Codex's trust prompt for us, by editing ~/.codex/config.toml
trust_launch_dirs = true

[layout]
# a wider list, since the workflow names are long
split = 0.3
secondary_split = 0.5

[keybindings.agents]
# keep n, add c for muscle memory from elsewhere
new = ["n", "c"]
# enter attaches; also allow l, since j and k already move
open = ["enter", "l"]

[keybindings.nav]
# vim keys plus the arrows
down = ["j", "down"]
up = ["k", "up"]

[actions]
# scripts live in dotfiles so they follow me between machines
dir = "~/dotfiles/weave/actions"

[context]
file = "~/dotfiles/weave/context.md"

[log]
level = "debug"
viewer = 'jq -c . "$WEAVE_LOG" | fzf --ansi --tac'
```

## A layered pair

The reason to split a config is that one half is worth sharing and the other is not. Here the base is the part you would check into a dotfiles repo, and the local file is the part that is true of one machine.

`~/.config/weave/config.toml`:

```toml
default_harness = "codex"
max_agents = 25

marks = [
  { glyph = "!", color = "red" },
  { glyph = "?", color = "yellow" },
  { glyph = "✓", color = "green" },
]

[harness.codex]
trust_launch_dirs = true

[keybindings.agents]
new = ["n", "c"]

[log]
level = "info"
```

`~/.config/weave/config.local.toml`, which stays on the machine:

```toml
database_url = "postgres://weave:secret@db.internal:5432/weave?sslmode=require"
cache_dir = "/fast-ssd/weave"
editor = "nvim"

[log]
level = "debug"
```

What weave ends up running: `log.level` is `debug` from the local file, and `log.viewer` is still the built-in, because merging a table does not disturb the keys it does not mention. `marks` is the base's three, since the local file said nothing about them. `keybindings.agents.new` is `["n", "c"]` and every other binding is its built-in default. `database_url` is the local file's, unless `DATABASE_URL` is exported in the shell you opened the panel from, in which case that wins.

## See also

[Installation](install.md) for getting Postgres and the tmux plugin in place. [Actions](actions.md) for what goes in the actions directory. [Workflows](workflows.md) for the flags and context an agent inherits above these defaults. [Development](development.md) for `mise run gen:config` and how the reference file is produced.
