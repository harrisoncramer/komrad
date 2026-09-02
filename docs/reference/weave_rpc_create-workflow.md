## weave rpc create-workflow

Create a workflow to group agents under.

### Synopsis

Create a workflow to group agents under. Nothing runs as a result; this is organisation. The cwd and flags become the defaults prefilled when an agent is created inside it, and the context is appended to those agents' system prompts. provisionAction names the action from list_actions that owns where each of its agents runs: it goes before the agent starts, it names the directory, and a failure abandons the launch. deprovisionAction names the half that gives that directory back once one of its agents is deleted, and is read only while provisionAction is set. Leave provisionAction blank for agents that keep the directory they are given, which retires both halves. worktree.enabled provisions every agent a git worktree off cwd instead: weave names both halves itself, so provisionAction must be blank when it is set. hooks hang scripts on a moment in its agents' lives rather than on their directories, each naming an event and an action: pre-launch runs in the agent's own directory before its pane opens and a failure abandons the create, post-launch runs once the CLI is in the pane and is not waited for. Several may hang on one moment and they run in the order given. worktree.diffExclude drops paths from the diff the panel shows for those agents, as relative paths or globs such as proto/gen or *.pb.go, and is read fresh every time a diff is drawn rather than snapshotted at launch. archiveOnPrClosed archives an agent once the pull request weave recorded for it merges or closes, checked every couple of minutes and only once that agent is idle or dormant, so nothing is taken out from under one mid-turn; the record, the transcript, and the worktree all survive, and restore_agents puts it back for good, since restoring an agent settles the pull request it carries and no later pass archives it for that one again. It works with or without a worktree, since the pull request it follows is only believed while it names the repository the workflow points at and the branch the agent's own checkout is on.

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
      --deprovision-action string        string, at most 255 characters
      --flags string                     string, at most 500 characters
      --harness string                   string, at most 40 characters, matching ^[a-z0-9-]*$
  -h, --help                             help for create-workflow
      --name string                      string, required, at most 200 characters
      --provision-action string          string, at most 255 characters
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

