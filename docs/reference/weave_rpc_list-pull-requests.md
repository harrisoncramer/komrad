## weave rpc list-pull-requests

List the pull requests opened from a branch of a repository, read through the forge connection for the repository's host.

### Synopsis

List the pull requests opened from a branch of a repository, read through the forge connection for the repository's host. Give the repository as a git remote spells it. A repository the connection's credential cannot see is answered as not found, which is also what a repository that does not exist answers.

```
weave rpc list-pull-requests [flags]
```

### Options

```
      --branch string         string, required, at most 512 characters
      --compact               print the response on one line
  -h, --help                  help for list-pull-requests
      --remote-url string     string, required, at most 2048 characters
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

