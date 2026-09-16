## weave rpc archive-workflow

Hide a workflow from the panel, archiving the agents inside it.

### Synopsis

Hide a workflow from the panel, archiving the agents inside it. Nothing is destroyed: the agents stay findable through search_transcripts and no transcript goes with them. Refused while any schedule still launches into the workflow, since those would have nowhere to run.

```
weave rpc archive-workflow [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for archive-workflow
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

