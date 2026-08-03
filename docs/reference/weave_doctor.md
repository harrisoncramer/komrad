## weave doctor

Check that this machine has everything weave needs

### Synopsis

Check that this machine has everything weave needs, one line per dependency.

It checks tmux and its version, the shell tmux starts panes with, whether the
config file parses and names a harness that exists, the database and whether its
schema is current, the read-only login the MCP query tools connect as, the service
on its address, and the CLI binary behind each harness. It repairs nothing, since a
command that changed what it was describing could not be trusted to say what is
wrong.

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
      --config string   path to config TOML
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

