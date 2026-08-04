## weave rpc list-workflows

List the workflows agents are grouped into, most recently updated first, with each one's shared context, the working directory and flags its agents start from, and the action run before its agents start and the one run after they are deleted.

### Synopsis

List the workflows agents are grouped into, most recently updated first, with each one's shared context, the working directory and flags its agents start from, and the action run before its agents start and the one run after they are deleted. Archived workflows are excluded unless archived is set, which lists those and only those. Returns one page; pass the response's nextPageToken back as pagination.cursor for the next.

```
weave rpc list-workflows [flags]
```

### Options

```
      --archived                   bool
      --compact                    print the response on one line
  -h, --help                       help for list-workflows
      --pagination.cursor string   string, at most 2048 characters
      --pagination.limit int32     int32
      --request-json string        the whole request as JSON, which the flags above are laid over
      --timeout duration           how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

