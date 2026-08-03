# Configuration

Two layers, and they do not overlap. A TOML file at `~/.config/weave/config.toml` holds the panel's settings: which CLI to run, per-CLI flags, keybindings, marks, layout, log level. A handful of environment variables hold the database URL, the listen address, the tmux socket, and the cache directory.

The rule of thumb: if you set it once and forget it, it goes in the file. If it identifies an instance, it is an environment variable.

## The config file

The file does not have to exist. With no file at all every default applies, which is the ordinary install. Write only the settings you are changing; anything you leave out falls back.

A key weave does not recognise is an error rather than a line quietly ignored, since a typo that does nothing is a setting you will spend twenty minutes deciding is broken. The error names the key and lists what the table takes:

```
max_agent is not a setting; the top level takes default_harness, max_agents,
harness, layout, marks, keybindings, actions, context, log
```

A file that will not parse puts an error screen in the panel and leaves the service running on defaults, so a broken config does not stop your agents, but your settings are not applied while it is broken. `weave doctor` says which file is being read and whether it parses.

`--config` names a different file, and is forwarded to the panel session and the service so all three read the same one. A `--config` path that is missing is an error, unlike the default path.

The `config.toml` at the root of the repo is generated from the Go struct and is a reference, not a template. Do not copy it into place: weave never reads it, and holding every key means a later rename breaks settings you never touched.

## Top level

`default_harness` is which CLI a new agent runs when neither the agent nor its [workflow](workflows.md) names one. Default `"claude"`, and it has to be one of the names under `[harness]`.

`max_agents` is how many unarchived agents may exist at once, default `50`. It is a backstop against a caller in a loop rather than a statement about your machine: drive weave from an orchestrator over [MCP](mcp.md) and a bug can otherwise spawn panes until tmux gives up.

`marks` is what the mark key cycles through, in order, and past the last one back to unmarked. Nothing but you ever sets a mark, so they mean whatever you decide.

```toml
marks = [
  { glyph = "!", color = "red" },
  { glyph = "?", color = "yellow" },
  { glyph = "✓", color = "green" },
]
```

A colour is one of the sixteen your terminal's scheme maps, named as `"red"` or `"brightred"`, or a hex value like `"#DCA561"`. Prefer a name, so the mark stays in the same scheme as the rest of the panel. The glyph is the mark's identity, and weave refuses one that is empty, over sixteen bytes, carries whitespace or control characters, or is listed twice.

## harness

A table per CLI weave implements, so this section doubles as the list of what can be run. A table for one it does not implement is an error.

```toml
[harness.claude]
flags = "--permission-mode acceptEdits"

[harness.codex]
flags = "-s workspace-write"
trust_launch_dirs = true
```

`flags` go ahead of whatever flags an agent carries itself. They are split and quoted before reaching the shell, so they are data; you cannot smuggle a pipeline through here.

`trust_launch_dirs` answers Codex's directory-trust question in advance, default `false`. Codex asks it the first time it runs somewhere new and only reads the answer from its own config, so an agent in a fresh worktree starts blocked and shows in the panel as waiting. Turning it on edits `~/.codex/config.toml` on your behalf.

## layout

`split` is the share of the width the panel's list takes, default `0.2`. The rest is the stage, where the agent you are hovering is drawn. `secondary_split` is the share of the stage's height taken by the pane that opens under it, default `0.4`, which is where the log, the shell, and the agent's record appear. Both are fractions above zero and below one.

```toml
[layout]
split = 0.3
secondary_split = 0.5
```

## keybindings

Bindings are scoped to the screen they act in, so the same key can mean different things in the agents list and the archive. Each is a list of key strings like `"enter"`, `"ctrl+d"`, `"esc"`, or `" "` for space.

Setting a binding replaces its default rather than adding to it, so list both if you want both. The fallback is per binding, so writing one key does not blank the others in its table.

```toml
[keybindings.agents]
new = ["n", "c"]
```

The groups are `actions`, `agents`, `archive`, `filter`, `form`, `help`, `logs`, `nav`, `schedules`, `selection`, and `workflows`. Four are shared rather than per screen: `nav` is movement everywhere, `help` draws the legend, `logs` opens the service log, `filter` narrows whichever list is on screen.

`?` in the panel draws the legend for the screen you are on, which is the honest list of what is bound right now. Two things are not configurable: the keys that close the filter input, since while it is open every other key types into the pattern, and the form's submit, which is `ctrl+s` because plain enter belongs to the text inputs.

## actions, context, log

```toml
[actions]
dir = "~/dotfiles/weave/actions"

[context]
file = "~/dotfiles/weave/context.md"

[log]
level = "debug"
viewer = 'jq -c . "$WEAVE_LOG" | fzf --ansi --tac'
```

`actions.dir` is where the panel finds the scripts its action key runs, default `~/.config/weave/actions`. One executable per action, named after it, and a file here overrides a built-in of the same name. See [actions](actions.md).

`context.file` is standing text every agent is started with whatever workflow it is in, default `~/.config/weave/context.md`. Read at each launch, so editing it reaches the next agent without restarting anything. The file not existing is fine.

`log.level` is one of `debug`, `info`, `warn`, `error`, default `"info"`. A level weave does not know is refused rather than decoded as debug.

`log.viewer` is the command the log key runs, default `less -R +F "$WEAVE_LOG"`. The path is exported as `WEAVE_LOG` rather than substituted into your command, so fzf's own `{}` still means what fzf means by it. Where the log is written is not configurable: it is `weave.log` under the cache directory, since a path the writer and the viewer could disagree about is a log nobody can find.

## Environment variables

These are read from the environment only, with no config file key.

`DATABASE_URL` is the Postgres weave stores everything in. Defaults to `postgres://weave:weave@localhost:5460/weave?sslmode=disable`, what `mise run docker:up` starts. Weave migrates the schema itself on startup.

`WEAVE_READONLY_DATABASE_URL` is the read-only login the [MCP](mcp.md) query tools connect as. Set nothing and weave derives it from `DATABASE_URL`, keeping the host, port, database, and options and putting the login `weave_readonly` in place of yours, so moving `DATABASE_URL` to another server moves both halves. Set it when that login needs its own password or reaches the database another way.

Weave connects twice because the MCP surface lets a model run SQL, and read-only stops writes and only writes; what a `SELECT` may see is decided by the role's privileges. It refuses to start if that login is a superuser, since a superuser is exempt from the checks on the catalogs and the file-reading functions, both reachable from a plain `SELECT`. Create the role with `weave readonly-role`, which grants through `DATABASE_URL` and needs `CREATEROLE`. `weave doctor` reports a pair landing on two different databases rather than refusing, since a pooler or a second hostname can be the same database under another name.

`WEAVE_ADDR` is where the service listens, default `127.0.0.1:8787`. Change it to run two weaves on one machine, or they will keep replacing each other's server.

`WEAVE_TMUX_SOCKET` is the socket weave runs its own tmux server on, default `weave`. Your normal tmux sees none of this; look at it with `tmux -L weave ls`.

`WEAVE_CACHE_DIR` is where weave keeps the log, the built-in action scripts it rewrites on every start, and the temp files a harness passes a prompt through. Everything under it is weave's to delete, so nothing of yours belongs there.

`EDITOR` is what the editor key opens an agent's directory in, default `vi`.

`WEAVE_AGENT_ID` and `WEAVE_SESSION_ID` are set by weave into every pane it launches so a call back through MCP is attributed to that agent. `TMUX` and `TMUX_PANE` come from tmux. Do not set any of these yourself.

## When changes take effect

A tmux server takes its environment once and hands that to every session afterwards, and weave's server is long-lived. So exporting a new `DATABASE_URL` and reopening the panel reaches nothing. Weave forwards the variables above with `-e` when it creates the manager session, but only the open that creates it does that.

After changing any of them:

```bash
weave stop
tmux -L weave kill-server
```

Then press `prefix + g` again. That kills the tmux server your agents run on, so anything mid-turn loses its pane. The records and transcripts are in Postgres and survive.

The TOML file is different. It is read at startup by the panel and the service, so it takes effect when you reopen the panel, without killing anything.

## A minimal config

Most people's is three or four lines. This one runs Codex by default and stops it asking about worktrees:

```toml
default_harness = "codex"

[harness.codex]
trust_launch_dirs = true

[harness.claude]
flags = "--permission-mode acceptEdits"
```

[Installation](install.md) covers Postgres and the tmux plugin, [actions](actions.md) what goes in the actions directory, [workflows](workflows.md) the flags and context an agent inherits above these defaults.
