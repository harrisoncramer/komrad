## weave stage

Draw a file into the panel's stage pane

### Synopsis

Draw a file into the panel's stage pane, redrawing it whenever a key arrives.

The panel runs this in the pane beside it and taps a key at it once it has rewritten
the file. It is not meant to be run by hand.

```
weave stage <file> [flags]
```

### Options

```
  -h, --help   help for stage
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

