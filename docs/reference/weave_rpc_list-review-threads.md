## weave rpc list-review-threads

Read the review conversations pinned to the lines of your own diff.

### Synopsis

Read the review conversations pinned to the lines of your own diff. Each thread names a file and a line range, quotes the diff lines it was written against, and holds the messages said in it so far, oldest first. This is where a person's feedback on your work actually lives: mail only tells you there is activity, it does not carry it. Set markRead to claim what you are shown, which is what stops the same threads being announced again, and leave it unset to look without claiming. Reply with add_review_message once you have made the change, saying what you did, and resolve_review_thread when the thread is settled. id defaults to your own agent; give another agent's id to read the review of its work instead.

```
weave rpc list-review-threads [flags]
```

### Options

```
      --agent-id string       string
      --compact               print the response on one line
  -h, --help                  help for list-review-threads
      --mark-read             bool
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

