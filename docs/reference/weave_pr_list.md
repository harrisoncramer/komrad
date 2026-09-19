## weave pr list

List the pull requests opened from a branch

### Synopsis

List the pull requests opened from a branch, through the forge its host is connected to.

This is the read the pull request sweep makes, run by hand. Give it a remote as git
spells it and a branch name:

    weave pr list git@github.com:harrisoncramer/weave.git agent/some-branch

A repository the credential cannot see answers not found, which is how a token
without access to a private repository shows up.

```
weave pr list <remote-url> <branch> [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave pr](weave_pr.md)	 - Read pull requests through the forges weave is connected to

