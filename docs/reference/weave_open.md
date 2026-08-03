## weave open

Open the panel popup, creating or attaching its persistent session

### Synopsis

Open the panel popup over the current tmux session.

It creates the manager session if it is not there and attaches either way, so the
panel keeps its place between opens rather than starting over. The manager session
lives on weave's own tmux server, so it does not show up in `tmux ls`.

The tmux plugin runs this as the popup's command, so it blocks until the client
detaches, which is what closes the popup. Pressing the panel key again runs
`weave hide`. Run by hand outside a popup it attaches the manager session in the
terminal instead, which is the panel without the popup around it.

```
weave open [flags]
```

### Options

```
  -h, --help   help for open
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

