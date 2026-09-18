## weave rpc restore-workflow

Take a workflow out of the archive, keyed by id, along with every agent archived inside it.

### Synopsis

Take a workflow out of the archive, keyed by id, along with every agent archived inside it. The agents come back as dormant rows, so call resume_agent on the ones whose conversations you want in a pane. Returns not_found if the workflow is not archived. It is refused whole rather than in part, either because its agents would not fit under the number the panel is configured to hold, or because live agents have taken their names, which the error lists for you to rename.

```
weave rpc restore-workflow [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for restore-workflow
      --id string             string, required
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

