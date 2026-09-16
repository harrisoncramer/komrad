## weave trace

Show every log line written under one trace id

### Synopsis

Show every log line weave wrote under one trace id.

A trace id covers one request end to end, or one pass of a background loop. The
service returns it on any call that failed, and it is on every line in the log, so
following what happened is a matter of naming it here.

Run with --recent instead of an id to list the traces the log ends with, which is
where to start when you know what you did but not what it was called.

```
weave trace [id] [flags]
```

### Options

```
  -h, --help     help for trace
      --recent   list the traces the log ends with rather than showing one
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

