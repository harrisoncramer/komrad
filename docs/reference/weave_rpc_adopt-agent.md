## weave rpc adopt-agent

Put an agent on work that already exists, naming a pull request by URL or number or naming a branch.

### Synopsis

Put an agent on work that already exists, naming a pull request by URL or number or naming a branch. The workflow decides the repository, so the pull request has to belong to the repository its cwd points at, and the workflow has to be one whose agents get a worktree of their own. Weave works out where the agent runs: a worktree on that branch when nothing holds it, or the checkout already holding it when a human made one by hand. The branch is the human's, so nothing here creates it, resets it, or deletes it afterwards. The pull request is recorded on the agent as it starts, and adopting the same one twice returns the agent already on it rather than a second one. Each call otherwise spawns another agent, so do not retry a call that may have succeeded.

```
weave rpc adopt-agent [flags]
```

### Options

```
      --compact                  print the response on one line
      --flags string             string, at most 500 characters
      --harness string           string, at most 40 characters, matching ^[a-z0-9-]*$
  -h, --help                     help for adopt-agent
      --idempotency-key string   string, at most 200 characters
      --prompt string            string, at most 50000 characters
      --ref string               string, required, at most 400 characters
      --request-json string      the whole request as JSON, which the flags above are laid over
      --setup-pane string        string, at most 20 characters
      --timeout duration         how long to wait for the call (default 30s)
      --title string             string, at most 200 characters
      --workflow-id string       string, required
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

