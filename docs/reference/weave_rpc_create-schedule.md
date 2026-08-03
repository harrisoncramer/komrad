## weave rpc create-schedule

Create a recurring agent launch inside a workflow.

### Synopsis

Create a recurring agent launch inside a workflow. Nothing runs at create time; the agent is spawned when the cron expression next matches, and again on every match after that. The workflow supplies the directory, flags, and shared context each launch uses, so only the prompt and the timing are set here. Create it with enabled false to write it down without arming it, or with hideAgents true to keep its runs out of the panel's list while they still run.

```
weave rpc create-schedule [flags]
```

### Options

```
      --compact               print the response on one line
      --cron string           string, required
      --enabled               bool
  -h, --help                  help for create-schedule
      --hide-agents           bool
      --name string           string, required, at most 200 characters
      --prompt string         string, required, at most 50000 characters
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
      --timezone string       string, at most 100 characters
      --workflow-id string    string, required
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

