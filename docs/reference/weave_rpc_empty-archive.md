## weave rpc empty-archive

Permanently delete every archived agent and workflow, tearing down each worktree that is still on disk.

### Synopsis

Permanently delete every archived agent and workflow, tearing down each worktree that is still on disk. It names nothing and cannot be undone, so read the archive first.

```
weave rpc empty-archive [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for empty-archive
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

