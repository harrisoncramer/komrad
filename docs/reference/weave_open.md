## weave open

Open the panel popup, creating or attaching its persistent session

### Synopsis

Open the panel popup over the current tmux session.

It creates the manager session if it is not there and attaches either way, so the
panel keeps its place between opens rather than starting over. The manager session
lives on weave's own tmux server, so it does not show up in `tmux ls`.

With engine files under the config's engines directory and no --engine given, it
first asks which weave to open, this machine or one of them, and runs itself again
on the one chosen. Closing that panel comes back to the question, and closing the
question closes the popup. An engine running other code than this weave cannot be
chosen.

The tmux plugin runs this as the popup's command, so it blocks until the client
detaches, which is what closes the popup. Pressing the panel key again runs
`weave hide`. Run by hand outside a popup it attaches the manager session in the
terminal instead, which is the panel without the popup around it.

With --detach it creates the session and returns, which is what a script wants when
it needs somewhere for agents to be put back into.

With --headless it creates that session holding nothing but a placeholder, for an
engine whose panel runs on somebody else's machine and which would otherwise swap
an agent's pane into a panel nobody is looking at.

```
weave open [flags]
```

### Options

```
      --detach     create the manager session if it is not there, then return without attaching
      --headless   create the manager session with no panel in it, for an engine driven from another machine
  -h, --help       help for open
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

