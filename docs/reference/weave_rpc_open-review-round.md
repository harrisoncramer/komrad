## weave rpc open-review-round

Freeze what an agent has written right now and get back the round to review against, which is a snapshot commit covering its whole worktree, committed and uncommitted alike.

### Synopsis

Freeze what an agent has written right now and get back the round to review against, which is a snapshot commit covering its whole worktree, committed and uncommitted alike. Call this before reading its diff, and diff the round's baseSha against its snapshotSha to see exactly what you are reviewing. A tree that has not moved since the last round gives that round back rather than a new one, so calling it twice is safe.

```
weave rpc open-review-round [flags]
```

### Options

```
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for open-review-round
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

