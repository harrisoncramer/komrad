## weave rpc list-agent-pull-requests

List the pull requests opened from an agent's branch in its repository, newest opened first, each with where it stands: open, approved, changes requested, draft, merged or closed.

### Synopsis

List the pull requests opened from an agent's branch in its repository, newest opened first, each with where it stands: open, approved, changes requested, draft, merged or closed. The one marked current is what the agent's row in the panel shows. This reads what the pull request sync has stored rather than asking the forge, so repository_read_at says how fresh it is, and an unset repository_read_at means the repository has not been read at all yet. An agent whose checkout is not on a forge repository answers an empty list.

```
weave rpc list-agent-pull-requests [flags]
```

### Options

```
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for list-agent-pull-requests
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

