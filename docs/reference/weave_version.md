## weave version

Print which build this binary is, and which one is serving

### Synopsis

Print which build this binary is, and which one is answering on the address.

The two can differ. The service is long-lived and keeps the binary it started with,
so rebuilding replaces the file on disk without changing what is running. When the
second line is not the first, restart the service.

```
weave version [flags]
```

### Options

```
  -h, --help   help for version
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

