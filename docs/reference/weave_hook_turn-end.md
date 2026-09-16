## weave hook turn-end

Record that an agent's turn ended

### Synopsis

Record that an agent's turn ended.

Codex runs this as its notify program, handing it one JSON argument. Weave keeps
the moment against the agent so the panel can say it has finished something
nobody has read. Anything but a completed turn is ignored.

```
weave hook turn-end [flags]
```

### Options

```
  -h, --help   help for turn-end
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave hook](weave_hook.md)	 - Run one of the agent CLI hooks weave installs

