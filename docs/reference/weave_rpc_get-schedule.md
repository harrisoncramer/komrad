## weave rpc get-schedule

Fetch one schedule by id.

### Synopsis

Fetch one schedule by id. Returns not_found if no schedule has that id.

```
weave rpc get-schedule [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for get-schedule
      --id string             string, required
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

