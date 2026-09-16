## weave rpc archive-agents

Stop a list of agents and hide them from the panel, keyed by id, keeping their records and transcripts.

### Synopsis

Stop a list of agents and hide them from the panel, keyed by id, keeping their records and transcripts. Prefer this to delete_agents: it is reversible with restore_agents, and search_transcripts still finds the conversations. The agents need not share a workflow. Returns not_found, and archives nothing at all, if any id given holds no agent, so a batch either goes whole or does not go. The children of any agent named here are archived with it, since a child rides along with its parent rather than standing on its own. A child named on its own is archived alone, which is how you finish with one reviewer while the agent it was reading for keeps working.

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
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

