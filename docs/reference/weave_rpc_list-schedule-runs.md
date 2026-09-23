## weave rpc list-schedule-runs

List a schedule's most recent firings, newest first, each with the occurrence it stands for, its state, its attempt count, and why it was skipped or how it failed.

### Synopsis

List a schedule's most recent firings, newest first, each with the occurrence it stands for, its state, its attempt count, and why it was skipped or how it failed. A completed run launched its agent, a cancelled one was skipped on purpose, a discarded one ran out of attempts, and a retryable one is still being tried. Firings are kept for 90 days. Returns not_found if no schedule has that id.

```
weave rpc list-schedule-runs [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for list-schedule-runs
      --id string             string, required
      --limit int32           int32, between 0 and 200
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

