## weave rpc release-review

Let go of the review you have been writing on another agent's diff, which is what sends it.

### Synopsis

Let go of the review you have been writing on another agent's diff, which is what sends it. Weave writes every comment out as one file and mails that agent the path, so the whole review arrives as one thing rather than a comment at a time. Give a verdict: approved if the round is good as it stands, changes requested if the agent should keep working, commented for notes without a conclusion. The verdict is what tells that agent whether it is done, so choose it deliberately. Interrupt stops it where it stands rather than waiting for the turn to end. Each call sends another review, so do not retry a call that may have succeeded.

```
weave rpc release-review [flags]
```

### Options

```
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for release-review
      --interrupt             bool
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
      --verdict string        REVIEW_VERDICT_APPROVED|REVIEW_VERDICT_CHANGES_REQUESTED|REVIEW_VERDICT_COMMENTED
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

