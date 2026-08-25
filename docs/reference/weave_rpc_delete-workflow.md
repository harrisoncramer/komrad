## weave rpc delete-workflow

Permanently delete an archived workflow along with whatever archived agents are still inside it, reusing the same cleanup-action handling a direct agent delete uses.

### Synopsis

Permanently delete an archived workflow along with whatever archived agents are still inside it, reusing the same cleanup-action handling a direct agent delete uses. Returns not_found if the workflow does not exist or is not archived. An agent whose cleanup action is still running, or whose cleanup failed, is left behind rather than blocking the call, so the workflow can stay archived and be tried again.

```
weave rpc delete-workflow [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for delete-workflow
      --id string             string, required
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

