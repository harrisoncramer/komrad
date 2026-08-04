## weave rpc get-config

Read the panel's configuration as it is currently in force, keyed the same way the user's config file is.

### Synopsis

Read the panel's configuration as it is currently in force, keyed the same way the user's config file is. Covers the default flags every new agent inherits and the keybindings for each of the panel's screens. Anything the user left out of their file reads back as the built-in default. A configError in the response means their file would not parse, in which case the configuration returned is the defaults the panel is running on instead. The version is the build the server answering you is running, which is worth quoting in a bug report about behaviour that makes no sense.

```
weave rpc get-config [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for get-config
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

