## weave rpc set-schedule-enabled

Pause or resume one schedule without editing anything else.

### Synopsis

Pause or resume one schedule without editing anything else. A paused schedule keeps its record and its history and stops firing.

```
weave rpc set-schedule-enabled [flags]
```

### Options

```
      --compact               print the response on one line
      --enabled               bool
  -h, --help                  help for set-schedule-enabled
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

