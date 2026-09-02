## weave rpc create-agent

Launch a background agent with an opening prompt.

### Synopsis

Launch a background agent with an opening prompt. Requires an absolute or ~-prefixed directory: this spawns a real agent in a real tmux window, and a relative path would point somewhere unpredictable. Each call spawns another agent, so do not retry a call that may have succeeded. There is a ceiling on how many agents may exist at once, and a call past it is refused as resource_exhausted; that means there is no room rather than something being broken, so stop and tell the user instead of trying again. Set parentAgentId to attach the new agent to an existing one as its child, which is the shape to reach for when the work needs a second pair of eyes on your own tree rather than an agent of its own: pass your own id from WEAVE_AGENT_ID. A child runs in its parent's directory and gets no worktree and no setup or cleanup action, so dir, setupAction and cleanupAction are all refused rather than quietly overridden, and workflowId is still required but has to be the parent's own. It may only mail its parent and its parent's other children, it cannot be archived or deleted on its own, and it goes when the parent goes. A child may not have children of its own.

```
weave rpc create-agent [flags]
```

### Options

```
      --cleanup-action string    string, at most 255 characters
      --compact                  print the response on one line
      --dir string               string, at most 4096 characters
      --flags string             string, at most 500 characters
      --harness string           string, at most 40 characters, matching ^[a-z0-9-]*$
  -h, --help                     help for create-agent
      --idempotency-key string   string, at most 200 characters
      --parent-agent-id string   string
      --prompt string            string, required, at most 50000 characters
      --request-json string      the whole request as JSON, which the flags above are laid over
      --setup-action string      string, at most 255 characters
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

