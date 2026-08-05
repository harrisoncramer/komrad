## weave rpc get-agent

Fetch one agent by id or by its short ref, including whatever metadata it has recorded about its own work.

### Synopsis

Fetch one agent by id or by its short ref, including whatever metadata it has recorded about its own work. A ref is the handle the panel prints beside a name, and the last few characters of one are enough as long as they name a single agent. Spans the archived agents too, since both handles outlive a row being archived. Returns not_found if nothing holds what you gave, and invalid_argument if it is too short or names more than one agent.

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
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

