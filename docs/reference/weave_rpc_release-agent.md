## weave rpc release-agent

Let an agent that hold_agent stopped carry on, telling it what happened while it was held.

### Synopsis

Let an agent that hold_agent stopped carry on, telling it what happened while it was held. Whatever you pass as said is typed into the agent as it starts again, which is how it learns that its files moved under it or that the work it was part-way through is no longer wanted; an empty said lets it go in silence, and said is ignored for an agent whose own turn had already ended. Releasing an agent nobody is holding is refused as failed_precondition, and so is one that could not be told to carry on, in which case the hold still stands and the agent is still stopped.

```
weave rpc release-agent [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for release-agent
      --id string             string, required
      --request-json string   the whole request as JSON, which the flags above are laid over
      --said string           string
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

