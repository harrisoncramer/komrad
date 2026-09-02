## weave rpc update-workflow

Replace a workflow's name, context, directory, flags, and the provisioner that owns its agents' directories.

### Synopsis

Replace a workflow's name, context, directory, flags, and the provisioner that owns its agents' directories. Every field is written, so read the workflow first and send back the ones you are not changing. Editing the context or provisionAction reaches only agents spawned afterwards, since existing agents took a snapshot at launch; editing deprovisionAction reaches agents already running, since it is read when it fires. worktree.enabled is the same decision spelled the other way: weave names both halves itself, so provisionAction must be blank when it is set, and turning it off clears both halves if they still name the generated scripts. worktree.diffExclude drops paths from the diff the panel shows for those agents, as relative paths or globs such as proto/gen or *.pb.go, and is read fresh every time a diff is drawn rather than snapshotted at launch. archiveOnPrClosed archives an agent once the pull request weave recorded for it merges or closes, checked every couple of minutes and only once that agent is idle or dormant, so nothing is taken out from under one mid-turn; the record, the transcript, and the worktree all survive, and restore_agents puts it back for good, since restoring an agent settles the pull request it carries and no later pass archives it for that one again. It works with or without a worktree, since the pull request it follows is only believed while it names the repository the workflow points at and the branch the agent's own checkout is on.

```
weave rpc update-workflow [flags]
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
  -h, --help                             help for update-workflow
      --id string                        string, required
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

