## weave rpc resolve-review-thread

Settle a review thread once you have done what it asked, or reopen one by passing resolved false.

### Synopsis

Settle a review thread once you have done what it asked, or reopen one by passing resolved false. Resolving is a claim that the feedback has been acted on, so say what you did with add_review_message first: a thread that closes with no answer in it tells the person reviewing nothing. A later message in the thread reopens it on its own.

```
weave rpc resolve-review-thread [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for resolve-review-thread
      --id string             string, required
      --request-json string   the whole request as JSON, which the flags above are laid over
      --resolved              bool
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

