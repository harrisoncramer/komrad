## weave panel

Run the panel itself, as the manager session's own command

### Synopsis

Run the panel itself.

This is the command the manager tmux session runs, not the one to type. It starts
the service if nothing is answering, then draws the panel and talks to that service
over the API like any other client. Run by hand outside the manager session it will
draw, but the popup and the key bindings around it will not be there.

Use `weave open` to bring the panel up.

```
weave panel [flags]
```

### Options

```
  -h, --help   help for panel
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

