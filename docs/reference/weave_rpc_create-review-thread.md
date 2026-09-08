## weave rpc create-review-thread

Pin a comment to a place in another agent's diff, which is how you give it a review rather than mailing it prose.

### Synopsis

Pin a comment to a place in another agent's diff, which is how you give it a review rather than mailing it prose. You may only review the agent you are a child of. The comment is filed under a batch of your own that nobody sees until you release it with release_review, so write every comment you have and then release once. Anchor it with the file and the line range from the round you are reviewing, and pass the diff lines you are writing about as quotedLines so the anchor still says something after the code moves. Pass suggestion to propose exact replacement lines the agent should apply. Each call opens another thread, so do not retry a call that may have succeeded.

```
weave rpc create-review-thread [flags]
```

### Options

```
      --agent-id string        string, required
      --base-sha string        string
      --body string            string, required, at most 10000 characters
      --compact                print the response on one line
      --file-path string       string, required
  -h, --help                   help for create-review-thread
      --new-end int32          int32
      --new-start int32        int32
      --old-end int32          int32
      --old-start int32        int32
      --quoted-lines strings   string, repeatable
      --request-json string    the whole request as JSON, which the flags above are laid over
      --suggestion strings     string, repeatable
      --timeout duration       how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

