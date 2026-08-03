## weave rpc set-agent-metadata

Record one fact about an agent's work under a key of your choosing, keyed by the agent's id, or clear that key by omitting the value.

### Synopsis

Record one fact about an agent's work under a key of your choosing, keyed by the agent's id, or clear that key by omitting the value. Use this on yourself as you work, whenever you learn something a person would want to jump to later: the Linear issue you are implementing, the pull request you opened, the branch you are on. Your own id is in the WEAVE_AGENT_ID environment variable. Store the full URL rather than a bare number, since the panel shows the value as it is. Keys are lowercase letters, digits, underscores, and dashes; call this once per key, which leaves every other key on the agent untouched. Do not record what the agent is currently doing, which is already its state, and do not paste findings here, which belong in your reply.

```
weave rpc set-agent-metadata [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for set-agent-metadata
      --id string             string, required
      --key string            string, at most 64 characters
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
      --value string          string
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

