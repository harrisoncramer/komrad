## weave rpc list-schedules

List the recurring agent launches, most recently updated first, with each one's cron expression, prompt, workflow, next run time, and the outcome of its last firing.

### Synopsis

List the recurring agent launches, most recently updated first, with each one's cron expression, prompt, workflow, next run time, and the outcome of its last firing. Returns one page; pass the response's nextPageToken back as pagination.cursor for the next. The summary this returns by default holds only the opening of each schedule's prompt, marked by promptTruncated; get_schedule returns one whole, and view VIEW_FULL returns every prompt on the page.

```
weave rpc list-schedules [flags]
```

### Options

```
      --compact                    print the response on one line
  -h, --help                       help for list-schedules
      --pagination.cursor string   string, at most 2048 characters
      --pagination.limit int32     int32
      --request-json string        the whole request as JSON, which the flags above are laid over
      --timeout duration           how long to wait for the call (default 11m0s)
      --view string                VIEW_SUMMARY|VIEW_FULL
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

