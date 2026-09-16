## weave rpc list-workflows

List the workflows agents are grouped into, in alphabetical order, with each one's shared context, the working directory and flags its agents start from, and the scripts hooked to moments in their agents' lives.

### Synopsis

List the workflows agents are grouped into, in alphabetical order, with each one's shared context, the working directory and flags its agents start from, and the scripts hooked to moments in their agents' lives. Archived workflows are excluded unless archived is set, which lists those and only those. Returns one page; pass the response's nextPageToken back as pagination.cursor for the next. The summary this returns by default holds only the opening of each workflow's context, marked by contextTruncated; get_workflow returns one whole, and view VIEW_FULL returns every context on the page.

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
      --view string                VIEW_SUMMARY|VIEW_FULL
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

