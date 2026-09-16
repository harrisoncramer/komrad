## weave rpc delete-mail

Permanently remove a list of mail, keyed by id.

### Synopsis

Permanently remove a list of mail, keyed by id. This does not claim the mail as read, it erases it: the recipient will never see it, whether or not it was claimed already. The batch either goes whole or does not go: it returns not_found, and deletes nothing at all, if any id given holds no mail.

```
weave rpc delete-mail [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for delete-mail
      --ids strings           string, required, repeatable
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

