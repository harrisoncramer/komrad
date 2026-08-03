## weave rpc update-workflow

Replace a workflow's name, context, directory, flags, and the actions run around its agents' lives.

### Synopsis

Replace a workflow's name, context, directory, flags, and the actions run around its agents' lives. Every field is written, so read the workflow first and send back the ones you are not changing. Editing the context or the setup action reaches only agents spawned afterwards, since existing agents took a snapshot at launch; editing the cleanup action reaches agents already running, since it is read when it fires.

```
weave rpc update-workflow [flags]
```

### Options

```
      --cleanup-action string   string, at most 255 characters
      --compact                 print the response on one line
      --context string          string, at most 50000 characters
      --cwd string              string, at most 4096 characters
      --flags string            string, at most 500 characters
      --harness string          string, at most 40 characters, matching ^[a-z0-9-]*$
  -h, --help                    help for update-workflow
      --id string               string, required
      --name string             string, required, at most 200 characters
      --request-json string     the whole request as JSON, which the flags above are laid over
      --setup-action string     string, at most 255 characters
      --timeout duration        how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

