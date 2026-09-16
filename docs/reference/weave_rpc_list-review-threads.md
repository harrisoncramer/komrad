## weave rpc list-review-threads

Read the review conversations pinned to the lines of a diff.

### Synopsis

Read the review conversations pinned to the lines of a diff. Each thread names a round, a file and a line range, quotes the diff lines it was written against, and holds the messages said in it so far, oldest first. The review you were sent is already in the file the mail named, so read that first; this is for looking a thread up again, and for answering one. Every thread belongs to a round, which is a frozen snapshot, so its line numbers are true of that round even after you have edited the file: run git diff against the round's shas to see what was reviewed, or search the file for quotedLines to find where that code sits now. Answer with add_review_message, saying what you did. You are shown only reviews that have been released, so a comment somebody is still writing is not here yet. agentId defaults to your own agent; give another agent's id to read the review of its work instead.

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
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

