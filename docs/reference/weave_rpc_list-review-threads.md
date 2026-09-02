## weave rpc list-review-threads

Read the review conversations pinned to the lines of your own diff.

### Synopsis

Read the review conversations pinned to the lines of your own diff. Each thread names a file and a line range, quotes the diff lines it was written against, and holds the messages said in it so far, oldest first. This is where a person's feedback on your work actually lives: mail only tells you there is activity, it does not carry it. The line numbers are where the comment was written rather than where that code is now, since your own edits since then have moved it. quotedLines is the text it was written against, so when the file no longer matches the line you are given, search the file for that text and answer the comment where it now sits rather than treating it as stale. Reply with add_review_message once you have made the change, saying what you did. You are shown only the comments the person has let go of, so a note they are still writing is not here yet and a fresh doorbell means there is more of the review to read. agentId defaults to your own agent; give another agent's id to read the review of its work instead.

```
weave rpc list-review-threads [flags]
```

### Options

```
      --agent-id string       string
      --compact               print the response on one line
  -h, --help                  help for list-review-threads
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

