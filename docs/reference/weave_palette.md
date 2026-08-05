## weave palette

Fire one of your actions on the agent whose pane this was opened over

### Synopsis

Open the command palette over an agent's pane.

The panel binds this to a key on its own tmux server, so pressing it inside an
agent's pane offers your actions for that agent without leaving the pane. Type to
narrow the list, enter runs what the cursor is on, and the popup closes.

Which agent it acts on comes from the pane the key was pressed in rather than from
anything passed here. Set `@weave_palette_key` in your tmux config to change the key.

```
weave palette [flags]
```

### Options

```
  -h, --help   help for palette
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

