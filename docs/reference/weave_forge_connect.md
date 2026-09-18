## weave forge connect

Connect a forge with a token read from stdin

### Synopsis

Connect a forge with a token read from stdin.

The token is read from standard input, or from the file --token-file names. It is
never taken as an argument, since arguments are visible to every process on the
machine. Pipe it in:

    gh auth token | weave forge connect github.com
    op read op://vault/github/token | weave forge connect github.com

Weave asks the forge who the token belongs to before storing anything, so a token
that is wrong, expired or scoped to nothing is refused here rather than found out
later. github.com and gitlab.com say which forge they run; any other host needs
--kind, and --api-url when the API is not at the host's usual place.

```
weave forge connect <host> [flags]
```

### Options

```
      --api-url string      where the forge answers API calls, for an enterprise host
  -h, --help                help for connect
      --kind string         which forge the host runs, github or gitlab, when the host does not say
      --token-file string   read the token from this file instead of stdin
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave forge](weave_forge.md)	 - Connect weave to the services pull requests live on

