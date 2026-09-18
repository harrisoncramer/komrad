## weave rpc list-hooks

List the hooks the user has written, which are executable files in their hooks directory.

### Synopsis

List the hooks the user has written, which are executable files in their hooks directory. A hook is a script or program that takes one agent as its input, and a workflow names one against a moment in its agents' lives, such as pre-launch or post-resume. The name is what create_workflow and update_workflow take in a hook's script field. A file listed with executable false cannot be run until the user chmods it, and the description and timeout are what the file's own header comments declare about it.

```
weave rpc list-hooks [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for list-hooks
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 11m0s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

