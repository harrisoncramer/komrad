## weave rpc get-agent

Fetch one agent by id, including whatever metadata it has recorded about its own work.

### Synopsis

Fetch one agent by id, including whatever metadata it has recorded about its own work. Spans the archived agents too, since an id is the one handle those still answer to. Returns not_found if no agent has that id.

```
weave rpc get-agent [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for get-agent
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

