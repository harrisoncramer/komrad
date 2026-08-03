## weave rpc create-agent

Launch a background agent with an opening prompt.

### Synopsis

Launch a background agent with an opening prompt. Requires an absolute or ~-prefixed directory: this spawns a real agent in a real tmux window, and a relative path would point somewhere unpredictable. Each call spawns another agent, so do not retry a call that may have succeeded. There is a ceiling on how many agents may exist at once, and a call past it is refused as resource_exhausted; that means there is no room rather than something being broken, so stop and tell the user instead of trying again.

```
weave rpc create-agent [flags]
```

### Options

```
      --compact                  print the response on one line
      --dir string               string, at most 4096 characters
      --flags string             string, at most 500 characters
      --harness string           string, at most 40 characters, matching ^[a-z0-9-]*$
  -h, --help                     help for create-agent
      --idempotency-key string   string, at most 200 characters
      --prompt string            string, required, at most 50000 characters
      --request-json string      the whole request as JSON, which the flags above are laid over
      --timeout duration         how long to wait for the call (default 30s)
      --title string             string, at most 200 characters
      --workflow-id string       string, required
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

