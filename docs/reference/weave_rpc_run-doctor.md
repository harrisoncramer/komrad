## weave rpc run-doctor

Check the machine the service runs on for what its agents need: tmux and its shell, the config, the harness binaries, the hooks and worktree directories, the database and its schema.

### Synopsis

Check the machine the service runs on for what its agents need: tmux and its shell, the config, the harness binaries, the hooks and worktree directories, the database and its schema. Each check answers ok, warn, fail or skip with what was found and what to do about it. This is what weave doctor asks an engine for, and it is how to find out whether an engine is fit to run agents without a shell on it.

```
weave rpc run-doctor [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for run-doctor
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

