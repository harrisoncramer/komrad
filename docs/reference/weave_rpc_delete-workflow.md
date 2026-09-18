## weave rpc delete-workflow

Permanently delete an archived workflow along with whatever archived agents are still inside it, reusing the same worktree-removal handling a direct agent delete uses.

### Synopsis

Permanently delete an archived workflow along with whatever archived agents are still inside it, reusing the same worktree-removal handling a direct agent delete uses. The archived schedules that launched into it go too, since nothing else ever removes them and they would keep the workflow alive. Returns not_found if the workflow does not exist or is not archived, and failed_precondition if a schedule inside it was moved from another workflow whose agents still hold it. An agent whose worktree is still being removed, or whose removal failed, is left behind rather than blocking the call, so the workflow can stay archived and be tried again.

```
weave rpc delete-workflow [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for delete-workflow
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

