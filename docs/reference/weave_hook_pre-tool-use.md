## weave hook pre-tool-use

Record the tool an agent is about to run

### Synopsis

Record the tool an agent is about to run.

Claude runs this before every tool call, handing it the call on stdin. Weave
keeps the name of the tool against the agent so the panel can say what it is
doing, and reads nothing else: a tool's input can carry a secret.

```
weave hook pre-tool-use [flags]
```

### Options

```
  -h, --help   help for pre-tool-use
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave hook](weave_hook.md)	 - Run one of the agent CLI hooks weave installs

