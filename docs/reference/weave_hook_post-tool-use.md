## weave hook post-tool-use

Record that an agent's tool call returned

### Synopsis

Record that an agent's tool call returned.

Claude runs this once a tool call has finished, which takes the tool back off
the agent's row. A call that fails runs nothing, so the row is left to the state
it is drawn beside rather than to this.

```
weave hook post-tool-use [flags]
```

### Options

```
  -h, --help   help for post-tool-use
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave hook](weave_hook.md)	 - Run one of the agent CLI hooks weave installs

