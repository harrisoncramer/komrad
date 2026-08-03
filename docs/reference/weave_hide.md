## weave hide

Hide the panel popup without stopping anything

### Synopsis

Hide the panel popup.

It detaches the client the popup is holding, which closes the popup. The manager
session stays up, so the next `weave open` comes back to the panel where it was, and
the service and the agents are untouched.

This is what the panel key runs while the popup is open, so it is rarely typed.

```
weave hide [flags]
```

### Options

```
  -h, --help   help for hide
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

