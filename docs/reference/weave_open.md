## weave open

Open the panel popup, creating or attaching its persistent session

### Synopsis

Open the panel popup over the current tmux session.

It creates the manager session if it is not there and attaches either way, so the
panel keeps its place between opens rather than starting over. The manager session
lives on weave's own tmux server, so it does not show up in `tmux ls`.

The tmux plugin runs this as the popup's command, so it blocks until the client
detaches, which is what closes the popup. Pressing the panel key again runs
`weave hide`.

It marks the popup open on your own tmux server and clears that mark on the way out,
which is the flag the panel key reads to decide between opening and hiding. A terminal
killed while this is attached never clears it, so the next press hides a popup that is
not there. That is why this is the key's command rather than one to type by hand:
`weave attach` puts the same session in a terminal without the popup around it,
and leaves the flag alone.

With --detach it creates the session and returns, which is what a script wants when
it needs somewhere for agents to be put back into.

```
weave open [flags]
```

### Options

```
      --detach   create the manager session if it is not there, then return without attaching
  -h, --help     help for open
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

