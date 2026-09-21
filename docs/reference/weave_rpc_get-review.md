## weave rpc get-review

Read one released review whole, which is what the mail announcing a release tells you to do with the review id it names.

### Synopsis

Read one released review whole, which is what the mail announcing a release tells you to do with the review id it names. It comes back in reading order: the verdict and what the reviewer said about the work as a whole, then each thread with the file and lines it is pinned to, the lines it quoted, the code around them as the round's snapshot held it with the commented lines marked, and the messages that went out with this release, so on a later round only the new comments are here. Those line numbers are true of the snapshot rather than of your files now, so once you have edited, search for the quoted text rather than counting lines. Answer each thread with add_review_message, naming its id; list_review_threads is for looking a thread up again afterwards. You may read a review of your own work and of the work of the agent you are a child of, and anybody else's is refused as permission_denied. A review nobody has released is not_found for anyone but its author.

```
weave rpc get-review [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for get-review
      --request-json string   the whole request as JSON, which the flags above are laid over
      --review-id string      string, required
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

