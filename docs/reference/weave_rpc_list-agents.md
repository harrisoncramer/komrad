## weave rpc list-agents

List the agents the panel tracks, live and dormant, with each one's name, state, working directory, flags, and workflow.

### Synopsis

List the agents the panel tracks, live and dormant, with each one's name, state, working directory, flags, and workflow. Archived agents are excluded unless archived is set, which lists those and only those. Returns one page, most recently updated first; pass the response's nextPageToken back as pagination.cursor for the next one. Start here to find the id or name another tool needs.

```
weave rpc list-agents [flags]
```

### Options

```
      --archived                   bool
      --compact                    print the response on one line
  -h, --help                       help for list-agents
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

