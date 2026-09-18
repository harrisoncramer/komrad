## weave rpc list-review-rounds

Read the rounds taken of an agent, oldest first.

### Synopsis

Read the rounds taken of an agent, oldest first. A round is a frozen snapshot of everything it had written at that moment, so this is how you find what a review was given against, and how you see what changed between two of them: git diff one round's snapshotSha against the next. Your own rounds are yours to read, which is how you see the code a comment on your work was written about even after you have edited it. Use open_review_round to take one of the work as it stands now.

```
weave rpc list-review-rounds [flags]
```

### Options

```
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for list-review-rounds
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 11m0s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

