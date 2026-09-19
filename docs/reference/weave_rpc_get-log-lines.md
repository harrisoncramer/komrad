## weave rpc get-log-lines

Read every log line the service wrote under one trace id, in order.

### Synopsis

Read every log line the service wrote under one trace id, in order. A trace id covers one request end to end or one pass of a background loop; the service answers it on any call that failed, and it is on every line in the log. This is how to find out what happened behind a failure, on whichever engine the call was made to.

```
weave rpc get-log-lines [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for get-log-lines
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
      --trace-id string       string, required, at most 128 characters
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

