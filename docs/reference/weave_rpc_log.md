## weave rpc log

Write one line to weave's log, tagged to the given agent id.

### Synopsis

Write one line to weave's log, tagged to the given agent id. An action fired on an agent runs with its own stdout and stdin sent nowhere, and its stderr read back only when it fails, so this is how a running action, or anything else acting on an agent's behalf, says what it is doing on purpose. Your own id is in the WEAVE_AGENT_ID environment variable.

```
weave rpc log [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for log
      --id string             string, required
      --message string        string, required, at most 2048 characters
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

