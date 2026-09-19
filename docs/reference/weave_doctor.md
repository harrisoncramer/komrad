## weave doctor

Check that this machine has everything weave needs

### Synopsis

Check that this machine has everything weave needs, one line per dependency.

It checks tmux and its version, the shell tmux starts panes with, whether the
config file parses and names a harness that exists, the hooks and worktrees
directories and whether any worktree there is left without an agent, the database
and whether its schema is current, the read-only login the MCP query tools connect
as, the service on its address, and the CLI binary behind each harness. It repairs
nothing, since a command that changed what it was describing could not be trusted
to say what is wrong.

It also asks the terminal what it can draw. Every glyph the agents list uses is
written out and the cursor position queried, so what comes back is how far the
cursor moved rather than how wide the ink was. Inside tmux that answer comes
from tmux's own grid rather than from the terminal underneath it. Bold and
italic are read out of the terminfo entry of the terminal tmux paints to, and
colour depth out of the entry this process emits for, which is what an entry
claims rather than what a terminal does. Nothing is asked of a terminal that is
not there, and whether the font has a glyph at all is not tested.

Pointed at an engine with --engine, the checks about this machine run here and the
ones about the engine are asked of its service, which makes them on its own
machine; they are printed under an "on the engine" heading, and marked not checked
when the service cannot be reached or is running a different build.

The exit status is non-zero when any check failed, so it can gate a script, and
--json prints the report rather than the summary. Run this first when the panel is
doing something that makes no sense.

```
weave doctor [flags]
```

### Options

```
      --color string   auto, always, or never (default "auto")
  -h, --help           help for doctor
      --json           print the raw report instead of the summary
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

