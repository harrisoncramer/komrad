## weave rpc set-agent-hidden

Hide an agent from the panel's list, keyed by its id, or unhide it by passing hidden false.

### Synopsis

Hide an agent from the panel's list, keyed by its id, or unhide it by passing hidden false. This changes nothing about the agent: it keeps running, the reconciler keeps moving its state, and every tool still returns it. It only decides whether the person watching the panel sees a row for it, which is theirs to decide, so do not hide work you were not asked to hide.

```
weave rpc set-agent-hidden [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for set-agent-hidden
      --hidden                bool
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

