## weave rpc update-schedule

Replace a schedule's name, cron expression, timezone, prompt, workflow, and whether the agents it launches start hidden from the panel.

### Synopsis

Replace a schedule's name, cron expression, timezone, prompt, workflow, and whether the agents it launches start hidden from the panel. Every field is written, so read the schedule first and send back the ones you are not changing. Agents already launched are untouched, including their own hidden flag.

```
weave rpc update-schedule [flags]
```

### Options

```
      --compact               print the response on one line
      --cron string           string, required
      --enabled               bool
  -h, --help                  help for update-schedule
      --hide-agents           bool
      --id string             string, required
      --name string           string, required, at most 200 characters
      --prompt string         string, required, at most 50000 characters
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 11m0s)
      --timezone string       string, at most 100 characters
      --workflow-id string    string, required
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

