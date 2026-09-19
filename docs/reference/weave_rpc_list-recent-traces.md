## weave rpc list-recent-traces

List the traces the service's log ends with, oldest first, each with when it started, what it was about and how many lines it holds.

### Synopsis

List the traces the service's log ends with, oldest first, each with when it started, what it was about and how many lines it holds. Start here when you know what you did but not what it was called, then read its lines with get_log_lines.

```
weave rpc list-recent-traces [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for list-recent-traces
      --limit int32           int32, between 0 and 200
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

