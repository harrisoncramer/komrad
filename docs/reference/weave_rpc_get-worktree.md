## weave rpc get-worktree

Read one worktree directory: its top-level entries with sizes, biggest first, what is uncommitted, the base it is measured against with how far ahead and behind it stands and how many of its commits the base already holds whole, whether deleting its agent would keep the branch, and the commits since the base.

### Synopsis

Read one worktree directory: its top-level entries with sizes, biggest first, what is uncommitted, the base it is measured against with how far ahead and behind it stands and how many of its commits the base already holds whole, whether deleting its agent would keep the branch, and the commits since the base. The base is read as the clone last fetched it unless fetch is set, which asks origin first. A directory whose registration git cannot read answers with the entries alone.

```
weave rpc get-worktree [flags]
```

### Options

```
      --compact               print the response on one line
      --dir string            string, required
      --fetch                 bool
  -h, --help                  help for get-worktree
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

