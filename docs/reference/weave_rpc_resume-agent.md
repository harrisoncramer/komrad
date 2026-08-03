## weave rpc resume-agent

Relaunch an agent's CLI on the same conversation, keyed by id, which is what reaches an agent whose pane has since died.

### Synopsis

Relaunch an agent's CLI on the same conversation, keyed by id, which is what reaches an agent whose pane has since died. An agent already running is returned as it stands rather than started twice. An archived agent is refused, so call restore_agent first.

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
      --config string   path to config TOML
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

