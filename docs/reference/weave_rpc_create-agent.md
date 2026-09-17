## weave rpc create-agent

Launch a background agent with an opening prompt.

### Synopsis

Launch a background agent with an opening prompt. Requires an absolute or ~-prefixed directory: this spawns a real agent in a real tmux window, and a relative path would point somewhere unpredictable. Each call spawns another agent, so do not retry a call that may have succeeded. There is a ceiling on how many agents may exist at once, and a call past it is refused as resource_exhausted; that means there is no room rather than something being broken, so stop and tell the user instead of trying again. Set parentAgentId to attach the new agent to an existing one as its child, passing your own id from WEAVE_AGENT_ID, which is the shape for an agent that reads the tree you are in: reach for a child whenever that is what the new agent does, and leave it off when the new agent writes work of its own and so needs a checkout of its own, or when the person asking said they wanted an agent standing on its own. Set provisionEnabled to false to keep the directory you named rather than be given one: that is how an agent reads a checkout somebody else already has open, and it is what the reviewer case wants. Leave it unset to do whatever the workflow does. A child runs in its parent's directory and provisions nothing of its own, so dir and provisionEnabled are both refused rather than quietly overridden, and workflowId is still required but has to be the parent's own. It may only mail its parent and its parent's other children, it goes when the parent goes, and it cannot be restored on its own while the parent stays archived. A child may not have children of its own, and neither may an orchestrator: it is drawn on its workflow's own row rather than a row of its own, so a child of it would be drawn nowhere at all, and a parentAgentId naming one is refused. An orchestrator fans read-only work out as an ordinary agent in its workflow with provisionEnabled false, which gives that agent a row, a mailbox and the workflow's own directory to read.

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
      --parent-agent-id string   string
      --prompt string            string, required, at most 50000 characters
      --provision-enabled        bool
      --request-json string      the whole request as JSON, which the flags above are laid over
      --timeout duration         how long to wait for the call (default 30s)
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

