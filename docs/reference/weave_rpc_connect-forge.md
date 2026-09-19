## weave rpc connect-forge

Connect weave to a forge host with a token, or as the GitHub App in the server's config.

### Synopsis

Connect weave to a forge host with a token, or as the GitHub App in the server's config. The credential is proved against the forge before anything is stored.

```
weave rpc connect-forge [flags]
```

### Options

```
      --api-url string        string
      --compact               print the response on one line
  -h, --help                  help for connect-forge
      --host string           string
      --kind string           string
      --method string         string
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
      --token string          string
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

