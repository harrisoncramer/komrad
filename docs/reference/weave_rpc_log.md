## weave rpc log

Write one line to weave's log, tagged to an agent.

### Synopsis

Write one line to weave's log, tagged to an agent. A script weave runs against an agent has its own stdout and stdin sent nowhere, and its stderr read back only when it fails, so this is how a running script, or anything else acting on an agent's behalf, says what it is doing on purpose. id defaults to your own, which is in the WEAVE_AGENT_ID environment variable; give another agent's id to log about it instead. script names the script whose run this line belongs to, which is in WEAVE_SCRIPT.

```
weave rpc log [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for log
      --id string             string
      --message string        string, required, at most 2048 characters
      --request-json string   the whole request as JSON, which the flags above are laid over
      --script string         string, at most 255 characters
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

