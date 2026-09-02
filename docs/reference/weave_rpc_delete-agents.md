## weave rpc delete-agents

Permanently remove a list of agents, keyed by id.

### Synopsis

Permanently remove a list of agents, keyed by id. This is a hard delete, not the panel's archive: the records and the transcripts go with them and search will never find the conversations again. The worktree of each one is removed with it, so a batch can tear down real directories. Returns not_found, and deletes nothing at all, if any id given holds no agent. Prefer archive_agents, and prefer leaving agents alone unless asked to remove them. The children of any agent named here are deleted with it, and a child named on its own is deleted alone, since it holds no directory of its own to tear down.

```
weave rpc delete-agents [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for delete-agents
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

