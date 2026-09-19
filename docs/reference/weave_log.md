## weave log

Show the log lines the service wrote under one trace id

### Synopsis

Show the log lines the service wrote under one trace id.

Every line the service logs carries a trace id, which is one id for one request end
to end or one pass of a background loop. A call that fails answers with its trace
id, so following what happened behind a failure is a matter of naming the id here.
The service reads its own log back, so this works against a remote engine the same
as against this machine.

Run with --recent instead of an id to list the trace ids the log ends with, which
is where to start when you know what you did but not what it was called.

```
weave log [trace-id] [flags]
```

### Options

```
  -h, --help     help for log
      --recent   list the trace ids the log ends with rather than showing one
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

