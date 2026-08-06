## weave attach

Attach the running panel in this terminal

### Synopsis

Attach the running panel in this terminal.

This is the panel without the popup around it, on the same session the panel key
opens, so both show the same thing and neither disturbs the other. The panel key
detaches it, which gives this terminal back and leaves everything running.

It attaches a panel that is already up and refuses when there is none, since
starting one is what the panel key is for.

```
weave attach [flags]
```

### Options

```
  -h, --help   help for attach
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

