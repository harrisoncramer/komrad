## weave rpc archive-schedule

Hide a schedule from the panel so it stops firing, keeping its record.

### Synopsis

Hide a schedule from the panel so it stops firing, keeping its record. The agents it already launched are left alone and keep running, and they still record the schedule that made them. Nothing brings an archived schedule back yet, so this is the end of it in practice.

```
weave rpc archive-schedule [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for archive-schedule
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

