## weave rpc adopt

Put an agent on work that already exists, naming the branch it is on.

### Synopsis

Put an agent on work that already exists, naming the branch it is on. A pull request URL or number is refused as invalid_argument, so read the branch off it and name that. The workflow decides the repository, and it has to be one whose agents get a worktree of their own. Weave works out where the agent runs: a worktree on that branch when nothing holds it, or the checkout already holding it when a human made one by hand. The branch is the human's, so nothing here creates it, resets it, or deletes it afterwards. A branch that already has an agent on it is refused as already_exists naming that agent, since git could not check it out twice anyway, so read the refusal rather than retrying. Each call otherwise spawns another agent, and only a repeat carrying the same idempotency_key is answered with the agent it already launched.

```
weave rpc adopt [flags]
```

### Options

```
      --compact                  print the response on one line
      --flags string             string, at most 500 characters
      --harness string           string, at most 40 characters, matching ^[a-z0-9-]*$
  -h, --help                     help for adopt
      --idempotency-key string   string, at most 200 characters
      --prompt string            string, at most 50000 characters
      --ref string               string, required, at most 400 characters
      --request-json string      the whole request as JSON, which the flags above are laid over
      --timeout duration         how long to wait for the call (default 11m0s)
      --title string             string, at most 200 characters
      --workflow-id string       string, required
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

