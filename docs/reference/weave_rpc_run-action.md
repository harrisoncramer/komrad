## weave rpc run-action

Fire one of the user's actions on an agent, naming the action as list_actions reports it and the agent by id.

### Synopsis

Fire one of the user's actions on an agent, naming the action as list_actions reports it and the agent by id. The action is started and nothing waits for it, so a successful call means the process began and says nothing about what it did or whether it worked. Where its output goes is the action's own business. An error means it never started: no such action, no such agent, or a file the user has not made executable. Your own id is in the WEAVE_AGENT_ID environment variable.

```
weave rpc run-action [flags]
```

### Options

```
      --action string         string, at most 255 characters
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for run-action
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

