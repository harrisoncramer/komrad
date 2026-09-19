## weave rpc list-worktrees

List every worktree directory weave holds, under the configured worktrees directory and each workflow's own, with the repository it was cut from, its branch, whether git can still read it, the agent it was made for and the agents attached to that one, and its size when a count has reached it.

### Synopsis

List every worktree directory weave holds, under the configured worktrees directory and each workflow's own, with the repository it was cut from, its branch, whether git can still read it, the agent it was made for and the agents attached to that one, and its size when a count has reached it. A directory with no agent is one whose row was deleted while cleanup did not run, or one a test left behind. This reads the directories and the agent rows and runs no git, so it is cheap to call often; read one directory's contents and standing against its base with get_worktree.

```
weave rpc list-worktrees [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for list-worktrees
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

