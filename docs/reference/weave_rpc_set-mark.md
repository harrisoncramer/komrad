## weave rpc set-mark

Put a single lowercase letter on an agent's row or a workflow's row, which is the shorthand a person types in the panel to jump straight to that row.

### Synopsis

Put a single lowercase letter on an agent's row or a workflow's row, which is the shorthand a person types in the panel to jump straight to that row. Name exactly one of agentId and workflowId, and leave mark out to take off whatever letter the row already answers to. A letter belongs to one row at a time, so claiming one another row already holds takes it off that row rather than being refused. Your own id is in the WEAVE_AGENT_ID environment variable, so this is how you label your own row while you work on something a person will want to come back to. The mark must be one lowercase character, and a row that is not live is refused as not_found. Which rows carry which letters is the person's own way around their sidebar, so do not move a letter you were not asked to move.

```
weave rpc set-mark [flags]
```

### Options

```
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for set-mark
      --mark string           string
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 11m0s)
      --workflow-id string    string, required
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

