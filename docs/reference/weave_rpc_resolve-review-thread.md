## weave rpc resolve-review-thread

Settle a review thread you wrote, once the answer to it is good enough, or reopen one you settled too early.

### Synopsis

Settle a review thread you wrote, once the answer to it is good enough, or reopen one you settled too early. Whether a review is finished is decided by its threads rather than by the verdict you gave, so this is how you say a finding is done with. You may only settle a comment you wrote yourself: a comment about your own work is not yours to settle, however well you think you have answered it, and saying so in the thread is what moves it instead. The person reviewing may settle anything, including a thread whose author is no longer running.

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

