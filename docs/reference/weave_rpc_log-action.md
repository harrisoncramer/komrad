## weave rpc log-action

Write a line to weave's log, tagged to the given agent.

### Synopsis

Write a line to weave's log, tagged to the given agent. This is how a running action reports progress on purpose: stderr is only read back when an action fails, so anything worth seeing while it runs, or after it succeeds, belongs here instead. Your own id is in the WEAVE_AGENT_ID environment variable.

```
weave rpc log-action [flags]
```

### Options

```
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for log-action
      --message string        string, required
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

