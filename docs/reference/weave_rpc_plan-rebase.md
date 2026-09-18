## weave rpc plan-rebase

Read what rebasing an agent's worktree onto its base would do, without moving anything.

### Synopsis

Read what rebasing an agent's worktree onto its base would do, without moving anything. Call this before rebase_worktree, every time: it names the ref and the commit the branch would land on, how many of the agent's own commits would be replayed, how many the base already holds whole and so would be dropped, how many merge commits a rebase would flatten away, and the paths a merge of the base could not settle. Those paths are a warning rather than a verdict, since a rebase replays commit by commit and can conflict where a merge would not. This refuses for the same reasons rebase_worktree does, so it is also how you find out whether a rebase is possible at all without attempting one.

```
weave rpc plan-rebase [flags]
```

### Options

```
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for plan-rebase
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

