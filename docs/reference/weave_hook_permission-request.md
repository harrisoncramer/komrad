## weave hook permission-request

Record the tool an agent is waiting to be allowed to run

### Synopsis

Record the tool an agent is waiting to be allowed to run.

Claude runs this when a tool call puts a dialog up for a person to answer.
Weave keeps the name of the tool against the agent, which is what tells a row
blocked on an approval from one blocked on a question, and reads nothing else:
a tool's input can carry a secret.

```
weave hook permission-request [flags]
```

### Options

```
  -h, --help   help for permission-request
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave hook](weave_hook.md)	 - Run one of the agent CLI hooks weave installs

