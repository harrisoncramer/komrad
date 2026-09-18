## weave rpc list-reviews

Read every review released on an agent, in the order their rounds were taken, each with the verdict it carried.

### Synopsis

Read every review released on an agent, in the order their rounds were taken, each with the verdict it carried. This is how you find where a change stands: whether the person reviewing has approved it, asked for changes, or only commented, and how many rounds it took to get there. The findings themselves are in the threads, which list_review_threads reads; this is the verdict on each round rather than the comments under it. agentId names whose reviews to read. The path of the file a review was written out to is dropped for anyone but the agent that wrote that review, so your own reviews come back with their paths and somebody else's do not. A review nobody has released is left out for the same reason, since what is in it is the reviewer's until they let go of it; a person running this from a terminal is drawn the open ones too. You may read your own reviews and those of the agent you are a child of, and anybody else's are refused as permission_denied.

```
weave rpc list-reviews [flags]
```

### Options

```
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for list-reviews
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

