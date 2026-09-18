## weave rpc add-review-message

Say something in a review thread, which is how you answer feedback on your own diff.

### Synopsis

Say something in a review thread, which is how you answer feedback on your own diff. Use it to report what you changed and why, or to ask what was meant when the note is not clear enough to act on. Your answer belongs to no batch and reaches the reviewer as soon as it is written, unlike a review, which is released whole. The message is attributed to you rather than to the person reviewing. Write it the way you would say it out loud. One or two sentences is the normal size and a hundred words is the ceiling. Name the line, the value or the case you mean, and quote code where the code is the point. No preamble, no summary at the end, no hedging, and no invented names for things the repository does not call that. Say what you changed, or what you did not change and why. Each call adds another message, so do not retry a call that may have succeeded.

```
weave rpc add-review-message [flags]
```

### Options

```
      --body string           string, required, at most 1000 characters
      --compact               print the response on one line
  -h, --help                  help for add-review-message
      --request-json string   the whole request as JSON, which the flags above are laid over
      --suggestion strings    string, repeatable
      --thread-id string      string, required
      --timeout duration      how long to wait for the call (default 11m0s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

