## weave rpc create-workflow

Create a workflow to group agents under.

### Synopsis

Create a workflow to group agents under. Nothing runs as a result; this is organisation. The cwd and flags become the defaults prefilled when an agent is created inside it, and the context is appended to those agents' system prompts. worktree.enabled gives every agent a git worktree of its own, cut from cwd, which weave makes before the agent starts and removes when it is deleted; left off, agents run in the directory they are given and weave provisions nothing. hooks hang your own scripts on a moment in those agents' lives, each naming an event and a hook from list_hooks: pre-launch runs in the agent's own directory before its pane opens and a failure abandons the create, while post-launch, post-resume and post-restore run in the background once the pane is up, the agent is back in a pane, or the archived row has come back. One hook per moment. worktree.diffExclude drops paths from the diff the panel shows for those agents, as relative paths or globs such as proto/gen or *.pb.go, and is read fresh every time a diff is drawn rather than snapshotted at launch. archiveOnPrClosed archives an agent once the pull request weave recorded for it merges or closes, checked every couple of minutes and only once that agent is idle or dormant, so nothing is taken out from under one mid-turn; the record, the transcript, and the worktree all survive, and restore_agents puts it back for good, since restoring an agent settles the pull request it carries and no later pass archives it for that one again. It works with or without a worktree, since the pull request it follows is only believed while it names the repository the workflow points at and the branch the agent's own checkout is on.

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
      --worktree.enabled                 bool
      --worktree.symlink-paths strings   string, repeatable
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

