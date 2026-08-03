# Configuration

Weave has two layers of configuration, and they do not overlap.

The first is a TOML file at `~/.config/weave/config.toml`. That is where the panel's own settings live: which agent CLI to run, per-CLI flags, keybindings, marks, layout, the log level.

The second is a handful of environment variables. That is where the database URL, the address the service listens on, the tmux socket, and the cache directory live. These decide which weave you are talking to, so they have to be settable per shell and per checkout, which a file in your home directory cannot do.

The rule of thumb: if you would set it once and forget it, it goes in the file. If it identifies an instance, it is an environment variable.

TODO TODO TODO: Write config file.

## The config file

Weave reads `~/.config/weave/config.toml`. It does not have to exist. With no file at all every default applies and the panel works fine, which is the ordinary install.

Write the file yourself and put only the settings you are changing in it. Anything you leave out falls back to its built-in default. There is no init command; `touch ~/.config/weave/config.toml` and start typing.

A key weave does not recognise is an error, not a line quietly ignored. That is deliberate: a typo that does nothing is a setting you will spend twenty minutes deciding is broken. The error names the key and lists what its table actually takes:

```
failed to parse config /Users/you/.config/weave/config.toml:
max_agent is not a setting; the top level takes default_harness, max_agents, harness, layout, marks, keybindings, actions, context, log, archive
```

When the file will not parse, the panel replaces itself with an error screen carrying that message, and the background service logs the same thing and carries on with the defaults. So a broken config does not stop your agents, but the flags you set are not being applied while it is broken. Fix the key and reopen the panel.

`weave doctor` tells you which file is being read, whether it parses, and whether `default_harness` names a CLI you actually have installed. Run that first when a setting is not taking effect.

### Pointing at a different file

`--config` names a file instead of the default. It is a persistent flag, so it goes before or after the subcommand:

```sh
weave --config /path/to/other.toml open
```

The path is forwarded to the panel session and to the background service it spawns, so all three read the same file. Useful for a second instance, or for testing a config without touching your real one.

A file that is named with `--config` and is not there is an error, unlike the default path. If you point at a file you believe you have, weave says it is missing rather than silently running on defaults.

### About config.toml in the repo

The `config.toml` at the root of the weave repository is generated. It is rendered from the `Config` struct in `internal/appconfig`, taking each key's description from the Go doc comment above it, by `mise run gen:config`.

It is a reference, not a template. Do not copy it into `~/.config/weave/`. Two reasons. Editing the repo file changes nothing, because weave never reads it, and the next `mise run gen:config` overwrites your edits. And copying all of it leaves you holding every key weave has, so a rename in a later version breaks your config even for settings you never touched.

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

## archive

### archive.purge_after_inactivity

How many hours an agent may sit in the archive before the service deletes it outright. Default `0`, which keeps the archive forever, so nothing changes for you unless you ask for it.

```toml
[archive]
purge_after_inactivity = 720
```

The hours count from when the agent was archived, since that is the point it stopped doing anything. An agent past the horizon is destroyed with its conversation, its search index and everything it recorded about its own work, which is the same delete the archive screen's delete key makes. Nothing brings it back.

Set it if your archive is growing faster than you will ever read it. The transcripts and the two indexes over them are most of what weave's database holds, and search walks every one of them, so an archive nobody empties makes search slower and the disk bigger for every run anybody ever finished.

The sweep runs hourly and writes what it took to the log, since it happens with nobody watching.

## Environment variables

These are read from the environment only. There is no config file key for them.

### DATABASE_URL

The Postgres weave stores everything in: agents, workflows, schedules, transcripts. Defaults to `postgres://weave:weave@localhost:5460/weave?sslmode=disable`, which is what `mise run docker:up` starts in this repo.

Weave brings the schema up to date itself when the service starts. There is no migration step to run by hand.

### WEAVE_READONLY_DATABASE_URL

A second login into the same database, for reading only.

Set nothing and weave derives it from `DATABASE_URL`, keeping the host, the port, the database, and the options exactly as you wrote them and putting the login `weave_readonly` in place of yours. So moving `DATABASE_URL` to another server moves both halves, and the two can only land on different databases if you say so here.

Weave needs two logins rather than one because the [MCP](mcp.md) surface lets a model run SQL. The connection is opened read-only, but read-only stops writes and only writes; what a `SELECT` is allowed to see is decided by the role's privileges. So the query tools connect as a role that has read on the application tables and nothing else.

Weave refuses to start if that URL lands on a superuser, and says so rather than warning, because a superuser is exempt from the privilege checks on the catalogs and on the file-reading functions, and both are reachable through a plain `SELECT` that writes nothing.

Set it when the read-only login needs a password of its own, or reaches the database by another route:

```sh
export DATABASE_URL='postgres://weave:secret@db.internal:5432/weave?sslmode=require'
export WEAVE_READONLY_DATABASE_URL='postgres://weave_readonly:other@db.internal:5432/weave?sslmode=require'
weave readonly-role
```

`weave readonly-role` creates the role named in the read-only URL, with the password in it, and grants it read on the application tables. It connects as `DATABASE_URL` and needs `CREATEROLE`. Run it once, and again after an upgrade that adds a table. It refuses to run if the two URLs name the same login, since that would be a read-only role with your write privileges.

`weave doctor` reports a pair naming two different databases, since both halves connect and nothing else would say the panel and the query tools are reading different rows. It reports rather than refuses, because an address weave cannot match up may still be the same database behind a connection pooler or a second name for the host.

### WEAVE_ADDR

Where the background service listens. Default `127.0.0.1:8787`. The panel, the CLI, and the MCP endpoint all talk to this address.

Change it when you run two weaves on one machine. Two instances on the same address will keep replacing each other's server, because the panel refuses to talk to a server built from a different version and restarts it.

`weave serve --addr` overrides it for a service started by hand.

### WEAVE_TMUX_SOCKET

The tmux socket weave runs its own server on. Default `weave`.

Weave does not use your tmux server. It starts a second one and puts the manager session and every agent pane on it, which is why nothing in your normal tmux sees any of this. To look at it directly:

```sh
tmux -L weave ls
```

Change this along with `WEAVE_ADDR` and `DATABASE_URL` when you want a second, isolated instance.

### WEAVE_CACHE_DIR

Where weave keeps what it generates: the service log, the built-in action scripts it writes out on every start, and the temp files a harness passes a prompt through. Defaults to `~/Library/Caches/weave` on a Mac and `~/.cache/weave` on Linux.

Everything under it is weave's to delete and rewrite, so nothing of yours belongs here. Set it if your cache directory is not writable, or to keep a second instance's log out of the first one's.

### EDITOR

The editor the panel's editor key opens an agent's directory in. Default `vi`. This is your existing environment variable and weave does not ask for it twice.

### Set by tmux and by weave

`TMUX` and `TMUX_PANE` are set by tmux, and weave reads them to work out which of your clients opened the popup. `WEAVE_AGENT_ID` and `WEAVE_SESSION_ID` are set by weave into every pane it launches, so a call an agent makes back through the MCP endpoint is attributed to that agent rather than to nobody. Do not set any of these yourself.

## When environment changes take effect

A tmux server takes its environment once, from whatever shell started it, and hands that to every session created afterwards. Weave's server is long-lived. So exporting a new `DATABASE_URL` and reopening the panel reaches nothing: the panel session already exists and is still running under an environment that could be weeks old.

Weave works around this by forwarding the six variables above with `-e` when it creates the manager session, so they pick up the shell that opened the popup. But only the open that creates the session does that. Every later one finds it already up.

So after changing any of them, restart:

```sh
weave stop
tmux -L weave kill-server
```

Then press `prefix + g` again. This kills the tmux server weave runs its agents on, so anything mid-turn loses its pane. The records and transcripts are in Postgres and survive.

The TOML file is different. It is read at startup by the panel and by the service, so changing it takes effect when you reopen the panel, without killing anything.

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

## See also

[Installation](install.md) for getting Postgres and the tmux plugin in place. [Actions](actions.md) for what goes in the actions directory. [Workflows](workflows.md) for the flags and context an agent inherits above these defaults. [Development](development.md) for `mise run gen:config` and how the reference file is produced.
