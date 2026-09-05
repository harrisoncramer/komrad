## weave rpc add-review-message

Say something in a review thread, which is how you answer feedback on your own diff.

### Synopsis

Say something in a review thread, which is how you answer feedback on your own diff. Use it to report what you changed and why, or to ask what was meant when the note is not clear enough to act on. Your answer belongs to no batch and reaches the reviewer as soon as it is written, unlike a review, which is released whole. The message is attributed to you rather than to the person reviewing. Each call adds another message, so do not retry a call that may have succeeded.

```
weave rpc add-review-message [flags]
```

### Options

```
      --body string           string, required, at most 10000 characters
      --compact               print the response on one line
  -h, --help                  help for add-review-message
      --request-json string   the whole request as JSON, which the flags above are laid over
      --suggestion strings    string, repeatable
      --thread-id string      string, required
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

