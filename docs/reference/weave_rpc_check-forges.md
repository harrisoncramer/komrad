## weave rpc check-forges

Ask every connected forge whether its credential still works, the way its method allows, and record the answer on each connection.

### Synopsis

Ask every connected forge whether its credential still works, the way its method allows, and record the answer on each connection. A refusal marks the connection as refused until a person connects it again.

```
weave rpc check-forges [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for check-forges
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

