## weave rpc list-pull-requests

List the pull requests weave has stored for a repository, a page at a time: every one opened from a branch when one is named, whatever its state, or every one stored for the repository when none is.

### Synopsis

List the pull requests weave has stored for a repository, a page at a time: every one opened from a branch when one is named, whatever its state, or every one stored for the repository when none is. The unfinished ones come first and each group is newest opened first, so the first answered for a branch is the one an agent's row on that branch stands for. Give the repository as a git remote spells it or as its canonical address, and pass the next_page_token back as pagination.cursor for the next page. This reads what the pull request sync has stored rather than asking the forge, so a repository no live agent sits on may be behind or empty; list_forge_pull_requests asks the forge itself.

```
weave rpc list-pull-requests [flags]
```

### Options

```
      --compact                    print the response on one line
      --head-branch string         string, at most 512 characters
  -h, --help                       help for list-pull-requests
      --pagination.cursor string   string, at most 2048 characters
      --pagination.limit int32     int32
      --repository-url string      string, required, at most 2048 characters
      --request-json string        the whole request as JSON, which the flags above are laid over
      --timeout duration           how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

