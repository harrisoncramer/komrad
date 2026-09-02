## weave rpc restore-agents

Take a list of agents out of the archive, keyed by id, along with the workflows they belong to.

### Synopsis

Take a list of agents out of the archive, keyed by id, along with the workflows they belong to. They come back as dormant rows and nothing is started, so call resume_agent on each one to put its conversation back in a pane. Agents that are not archived are returned as they stand. The batch either goes whole or does not go: it returns not_found if any id holds no agent, already_exists when a live agent has taken a returning agent's name or two returning agents want one name, and resource_exhausted when the panel has no room for all of them at once. The children of any agent named here come back with it, and a child cannot be restored on its own.

```
weave rpc restore-agents [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for restore-agents
      --ids strings           string, required, repeatable
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

