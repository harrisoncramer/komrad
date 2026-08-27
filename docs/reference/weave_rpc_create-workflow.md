## weave rpc create-workflow

Create a workflow to group agents under.

### Synopsis

Create a workflow to group agents under. Nothing runs as a result; this is organisation. The cwd and flags become the defaults prefilled when an agent is created inside it, and the context is appended to those agents' system prompts. setupAction names one action from list_actions to run before each of its agents starts, which blocks and abandons the launch if it fails; cleanupAction names the one run once one of its agents is deleted. Both are blank for none. worktree.enabled gives every agent its own git worktree off cwd instead: weave generates and names the two actions itself, so setupAction and cleanupAction must be blank when it is set. worktree.diffExclude drops paths from the diff the panel shows for those agents, as relative paths or globs such as proto/gen or *.pb.go, and is read fresh every time a diff is drawn rather than snapshotted at launch.

```
weave rpc create-workflow [flags]
```

### Options

```
      --cleanup-action string            string, at most 255 characters
      --compact                          print the response on one line
      --context string                   string, at most 50000 characters
      --cwd string                       string, at most 4096 characters
      --flags string                     string, at most 500 characters
      --harness string                   string, at most 40 characters, matching ^[a-z0-9-]*$
  -h, --help                             help for create-workflow
      --name string                      string, required, at most 200 characters
      --request-json string              the whole request as JSON, which the flags above are laid over
      --setup-action string              string, at most 255 characters
      --timeout duration                 how long to wait for the call (default 30s)
      --worktree.base-branch string      string, at most 255 characters
      --worktree.copy-paths strings      string, repeatable
      --worktree.diff-exclude strings    string, repeatable
      --worktree.enabled                 bool
      --worktree.symlink-paths strings   string, repeatable
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

