## weave forge check

Ask every connected forge whether its credential still works

### Synopsis

Ask every connected forge whether its credential still works, the way its method
allows, and record the answer on each connection. A refusal marks the connection
until it is connected again.

```
weave forge check [flags]
```

### Options

```
  -h, --help   help for check
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave forge](weave_forge.md)	 - Connect weave to the services pull requests live on

