## weave rpc delete-worktrees

Queue one background job that deletes worktree directories, each by the path its row decides.

### Synopsis

Queue one background job that deletes worktree directories, each by the path its row decides. A directory whose agent is archived goes through delete_agents, which runs the workflow's teardown hooks and the cleanup and takes the agent's children with it; one nothing answers for, or whose agent's own delete failed on a registration git cannot read, goes through remove_unowned_worktrees; one whose agent is live is left alone. It answers once the job is recorded, before anything has gone. A directory the listing no longer holds is skipped, so a retried job takes only what is left, and one that is refused stays listed by list_worktrees with the reason in the log.

```
weave rpc delete-worktrees [flags]
```

### Options

```
      --compact               print the response on one line
      --dirs strings          string, required, repeatable
  -h, --help                  help for delete-worktrees
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

