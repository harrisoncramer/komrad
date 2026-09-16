## weave hook stop

Deliver an agent's unread mail as its turn ends

### Synopsis

Deliver an agent's unread mail as its turn ends.

The agent CLI runs this on its Stop event. It reads the agent's mail and prints it
back as additional context, which the CLI hands the model as another turn. An
agent with no mail, or a panel that cannot be reached, prints nothing and lets the
turn end.

```
weave hook stop [flags]
```

### Options

```
  -h, --help   help for stop
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave hook](weave_hook.md)	 - Run one of the agent CLI hooks weave installs

