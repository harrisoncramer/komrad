## weave rpc list-schedules

List the recurring agent launches, most recently updated first, with each one's cron expression, prompt, workflow, and next run time.

### Synopsis

List the recurring agent launches, most recently updated first, with each one's cron expression, prompt, workflow, and next run time. Returns one page; pass the response's nextPageToken back as pagination.cursor for the next.

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
      --timeout duration           how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

