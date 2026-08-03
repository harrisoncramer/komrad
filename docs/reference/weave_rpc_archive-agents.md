## weave rpc archive-agents

Stop a list of agents and hide them from the panel, keyed by id, keeping their records and transcripts.

### Synopsis

Stop a list of agents and hide them from the panel, keyed by id, keeping their records and transcripts. Prefer this to delete_agents: it is reversible with restore_agents, and search_transcripts still finds the conversations. The agents need not share a workflow. Returns not_found, and archives nothing at all, if any id given holds no agent, so a batch either goes whole or does not go.

```
weave rpc archive-agents [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for archive-agents
      --ids strings           string, required, repeatable
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

