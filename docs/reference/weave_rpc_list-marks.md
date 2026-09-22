## weave rpc list-marks

List every mark, by letter, with the agent or the workflow each one jumps to.

### Synopsis

List every mark, by letter, with the agent or the workflow each one jumps to. A mark is the shorthand a person types in the panel to reach a row, and a letter belongs to one row at a time.

```
weave rpc list-marks [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for list-marks
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

