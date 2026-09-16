## weave rpc request-review

Get your own work read before you call it finished, which spawns the reviewer for you and hands you back the rest of the loop.

### Synopsis

Get your own work read before you call it finished, which spawns the reviewer for you and hands you back the rest of the loop. Commit on your branch first, since a review is given against a snapshot of your checkout as it stands. The reviewer is launched as your child, in your checkout, marked as a reviewer so it is briefed to read rather than to write, and named after you unless you pass a title. Your prompt is the whole briefing it gets, so say what the change is meant to do and where you are unsure of it rather than only naming a branch. agentId defaults to your own, which is in the WEAVE_AGENT_ID environment variable. One reviewer reads every round of a change, so call this once: a second ask while that reviewer is still owed an answer is refused as already_exists, and what tells it there is more to read is you answering its threads with add_review_message. Pass idempotencyKey to make a retry safe, since a repeat of the call that spawned the reviewer is answered with that reviewer rather than refused. Once a reviewer has released an approval it is finished, so the next piece of work gets a reviewer of its own. Returns failed_precondition when the agent cannot hold a reviewer, which a child and a reviewer both cannot, and resource_exhausted when the panel has no room for another agent, which means to carry on and say so rather than to try again.

```
weave rpc request-review [flags]
```

### Options

```
      --agent-id string          string
      --compact                  print the response on one line
  -h, --help                     help for request-review
      --idempotency-key string   string, at most 200 characters
      --prompt string            string, required, at most 50000 characters
      --request-json string      the whole request as JSON, which the flags above are laid over
      --timeout duration         how long to wait for the call (default 30s)
      --title string             string, at most 200 characters
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

