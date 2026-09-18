## weave rpc rebase-worktree

Move an agent's branch onto the latest commit of its base, rewriting the branch in the checkout it sits in.

### Synopsis

Move an agent's branch onto the latest commit of its base, rewriting the branch in the checkout it sits in. Reach for this when an agent's worktree has fallen behind the base and its work has to sit on top of what has landed since, rather than driving git by hand: weave knows which base the agent's workflow cuts from, fetches it, parks the whole checkout at a ref of its own first so nothing is lost, replays from a cutoff when the base already holds part of the branch, and mails the agent to say its files moved under it. Read plan_rebase first, since this rewrites history and no pull request opened from the branch tracks the work afterwards. You cannot rebase yourself: an agent part-way through a turn is refused as failed_precondition, and an agent is part-way through a turn whenever it is calling a tool, so this reaches an agent that has come to rest and nobody else. It is refused for the same reason for an agent that is held, archived, on a branch weave did not cut, holding uncommitted tracked changes, or already part-way through a merge or a rebase. A rebase that hits conflicts stops part-way and is left for that agent to settle, which the response says rather than failing.

```
weave rpc rebase-worktree [flags]
```

### Options

```
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for rebase-worktree
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

