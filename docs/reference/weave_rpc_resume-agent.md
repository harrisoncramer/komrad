## weave rpc resume-agent

Relaunch an agent's CLI on the same conversation, keyed by id, which is what reaches an agent whose pane has since died.

### Synopsis

Relaunch an agent's CLI on the same conversation, keyed by id, which is what reaches an agent whose pane has since died. An agent already running is returned as it stands rather than started twice. An archived agent is refused, so call restore_agent first, and one this panel is already part-way through resuming is refused as failed_precondition, which means to wait for the pane rather than to call again.

```
weave rpc resume-agent [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for resume-agent
      --id string             string, required
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

