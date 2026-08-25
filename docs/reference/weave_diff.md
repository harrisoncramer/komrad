## weave diff

Review what an agent has changed in its worktree

### Synopsis

Review what an agent has changed in its worktree.

The diff is measured against the point the agent's branch left its workflow's base
branch, so committed and uncommitted work show together, along with any file the
agent has written and not yet added.

The panel binds this to a key on the agents list, which opens it as a popup over
whatever you are looking at. Move with j and k, jump between files with ] and [,
pan sideways with h and l, and press q to close.

```
weave diff <agent> [flags]
```

### Options

```
  -h, --help   help for diff
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

