## weave rpc list-forges

List the forges weave is connected to, one per host, with who weave acts as there and when the forge last confirmed it.

### Synopsis

List the forges weave is connected to, one per host, with who weave acts as there and when the forge last confirmed it. The token itself is never returned. A connection is made by a person with weave forge connect and is what the pull request sweep reads through.

```
weave rpc list-forges [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for list-forges
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

