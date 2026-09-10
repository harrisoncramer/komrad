## weave rpc create-workflow

Create a workflow to group agents under.

### Synopsis

Create a workflow to group agents under. Nothing runs as a result; this is organisation. The cwd and flags become the defaults prefilled when an agent is created inside it, and the context is appended to those agents' system prompts. worktree.enabled gives every agent a git worktree of its own, cut from cwd, which weave makes before the agent starts and removes when it is deleted; left off, agents run in the directory they are given and weave provisions nothing. hooks hang your own scripts on a moment in those agents' lives, one per moment, each naming a hook from list_hooks and one of pre-launch, post-launch, post-resume, post-restore or pre-teardown; only pre-launch and pre-teardown are waited for, a pre-launch failure abandoning the create and a pre-teardown failure holding the delete back with the directory still there. worktree.dir names the parent new worktrees are made in, blank meaning the one every workflow shares. worktree.diffExclude drops paths from the diff the panel draws, as paths or globs relative to the checkout such as proto/gen or *.pb.go. archiveOnPrClosed archives an agent once weave sees the pull request it recorded merge or close. docs/workflows.md says why each of the three is there.

No flag is derived for hooks; pass those with --request-json.

```
weave rpc create-workflow [flags]
```

### Options

```
      --archive-on-pr-closed             bool
      --compact                          print the response on one line
      --context string                   string, at most 50000 characters
      --cwd string                       string, at most 4096 characters
      --flags string                     string, at most 500 characters
      --harness string                   string, at most 40 characters, matching ^[a-z0-9-]*$
  -h, --help                             help for create-workflow
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
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

