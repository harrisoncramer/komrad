## weave rpc update-workflow

Replace a workflow's name, context, directory, flags, worktree config and hooks.

### Synopsis

Replace a workflow's name, context, directory, flags, worktree config and hooks. Every field is written, so read the workflow first and send back the ones you are not changing, the hooks list included. Editing the context reaches only agents spawned afterwards, since existing agents took a snapshot at launch, while worktree.enabled reaches agents already running, since whether weave removes an agent's directory is read when the agent is deleted. worktree.dir names the parent new worktrees are made in, blank meaning the one every workflow shares. worktree.diffExclude drops paths from the diff the panel draws, as paths or globs relative to the checkout such as proto/gen or *.pb.go. docs/workflows.md says why each is there.

No flag is derived for hooks; pass those with --request-json.

```
weave rpc update-workflow [flags]
```

### Options

```
      --compact                          print the response on one line
      --context string                   string, at most 50000 characters
      --cwd string                       string, at most 4096 characters
      --flags string                     string, at most 500 characters
      --harness string                   string, at most 40 characters, matching ^[a-z0-9-]*$
  -h, --help                             help for update-workflow
      --id string                        string, required
      --name string                      string, required, at most 200 characters
      --request-json string              the whole request as JSON, which the flags above are laid over
      --timeout duration                 how long to wait for the call (default 30s)
      --worktree.base-branch string      string, at most 255 characters
      --worktree.copy-paths strings      string, repeatable
      --worktree.diff-exclude strings    string, repeatable
      --worktree.dir string              string, at most 4096 characters
      --worktree.enabled                 bool
      --worktree.symlink-paths strings   string, repeatable
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

