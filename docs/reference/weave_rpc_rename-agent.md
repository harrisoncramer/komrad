## weave rpc rename-agent

Rename an agent, keyed by its id rather than its current name.

### Synopsis

Rename an agent, keyed by its id rather than its current name. The name is a display label the store keys on; the running tmux window keeps whatever it was called at launch. It may not contain spaces, colons, dots, slashes, or control characters.

```
weave rpc rename-agent [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for rename-agent
      --id string             string, required
      --name string           string, required, at most 200 characters
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

