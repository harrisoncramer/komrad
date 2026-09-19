## weave rpc remove-unowned-worktrees

Remove worktree directories no agent answers for, which delete_agents cannot reach: a directory under the worktrees directories whose agent row is gone, or whose row is in failed_to_cleanup because git can no longer read the registration.

### Synopsis

Remove worktree directories no agent answers for, which delete_agents cannot reach: a directory under the worktrees directories whose agent row is gone, or whose row is in failed_to_cleanup because git can no longer read the registration. Each directory goes and, where the repository can still be read, git worktree prune runs in it; no branch is ever touched, since without a row nothing says whether the branch was adopted. A directory outside the worktrees directories, or one a live or archived agent still answers for, is refused with the reason and the rest go; delete the agent instead for those. Read list_worktrees first and pass only the directories it reports with no agent.

```
weave rpc remove-unowned-worktrees [flags]
```

### Options

```
      --compact               print the response on one line
      --dirs strings          string, required, repeatable
  -h, --help                  help for remove-unowned-worktrees
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

