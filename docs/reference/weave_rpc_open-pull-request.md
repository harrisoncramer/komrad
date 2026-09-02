## weave rpc open-pull-request

Open the pull request an agent recorded for itself, in the browser of whoever is at the panel.

### Synopsis

Open the pull request an agent recorded for itself, in the browser of whoever is at the panel. It is the panel's own key spelled as a call, so it answers as soon as the browser has been asked and says nothing about what the browser did. An error means it never got that far: no such agent, or no pull request recorded on it.

```
weave rpc open-pull-request [flags]
```

### Options

```
      --agent-id string       string, required
      --compact               print the response on one line
  -h, --help                  help for open-pull-request
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

