## weave rpc start-orchestrator

Bring a workflow's orchestrator back with a pane to talk to, starting the one that workflow has never had.

### Synopsis

Bring a workflow's orchestrator back with a pane to talk to, starting the one that workflow has never had. An orchestrator is the agent drawn on the workflow's own row rather than a row of its own, and it hands work out across that workflow rather than writing any itself. This returns whichever orchestrator the workflow already has, taking it out of the archive and resuming its conversation as far as either is needed, and launches a fresh one only when the workflow has never had one hold a conversation at all. An orchestrator already running comes back as it stands rather than being started twice, so this is safe to call again. A workflow that does not exist is refused as not_found.

```
weave rpc start-orchestrator [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for start-orchestrator
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 11m0s)
      --workflow-id string    string, required
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

