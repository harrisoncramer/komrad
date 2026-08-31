## weave rpc adopt-agent

Take over a CLI conversation that already exists, naming the session id of a Claude or Codex conversation somebody started outside the panel.

### Synopsis

Take over a CLI conversation that already exists, naming the session id of a Claude or Codex conversation somebody started outside the panel. The harness is worked out from where the conversation's log lives, and the agent runs in the directory that conversation was held in. Claude conversations are forked rather than continued: the panel mints a session of its own carrying the whole history, so the person's own conversation is left exactly as it stands. Codex has no fork, so adopting one appends to the person's own rollout, which is a takeover rather than a copy. Adopting the same conversation twice returns the agent already on it. Each call otherwise spawns another agent, so do not retry a call that may have succeeded.

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
      --request-json string      the whole request as JSON, which the flags above are laid over
      --session-id string        string, required, at most 200 characters, matching ^[A-Za-z0-9_-]+$
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

