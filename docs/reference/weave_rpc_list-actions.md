## weave rpc list-actions

List the actions the user has written, which are executable files in their actions directory.

### Synopsis

List the actions the user has written, which are executable files in their actions directory. An action is a script or program that takes one agent as its input and does whatever the user wrote it to do, such as opening the pull request that agent recorded. The name is what run_action takes. A file listed with executable false cannot be run until the user chmods it, and the description and timeout are what the file's own header comments declare about it.

```
weave rpc list-actions [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for list-actions
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

