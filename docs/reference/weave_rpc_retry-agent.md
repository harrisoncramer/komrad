## weave rpc retry-agent

Retry launching an agent that failed to create, keyed by id, optionally with corrected flags, prompt, or harness.

### Synopsis

Retry launching an agent that failed to create, keyed by id, optionally with corrected flags, prompt, or harness. Only works on agents in the failed_to_create state. An archived agent is refused. Returns the agent as relaunched.

```
weave rpc retry-agent [flags]
```

### Options

```
      --compact               print the response on one line
      --flags string          string, at most 500 characters
      --harness string        string, at most 40 characters, matching ^[a-z0-9-]*$
  -h, --help                  help for retry-agent
      --id string             string, required
      --prompt string         string, at most 50000 characters
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

