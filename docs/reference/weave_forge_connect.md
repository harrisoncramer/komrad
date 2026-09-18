## weave forge connect

Connect a forge with a token read from stdin, or as the engine's GitHub App

### Synopsis

Connect a forge with a token read from stdin, or as the engine's GitHub App.

With --method token, the default, the token is read from standard input, or from
the file --token-file names. It is never taken as an argument, since arguments are
visible to every process on the machine. Pipe it in:

    gh auth token | weave forge connect github.com
    op read op://vault/github/token | weave forge connect github.com

With --method github-app nothing is read. Weave mints a token from the App named
under [github_app], checks the installation reaches at least one repository, and
records the App's own name as the login. That is the engine's path, where nobody
is there to paste a token and the App's key is already on the machine.

Weave asks the forge who the token belongs to before storing anything, so a token
that is wrong or expired is refused here rather than found out later. That proves
the token is real, not that it can read your repositories: a token with no
permissions answers the same question, and shows up later as a repository the
forge says it cannot find. github.com and gitlab.com say which forge they run; any
other host needs --kind, and --api-url when the API is not at https://<host>/api/v3
for GitHub or /api/v4 for GitLab. The address has to be https.

```
weave forge connect <host> [flags]
```

### Options

```
      --api-url string      where the forge answers API calls, for an enterprise host
  -h, --help                help for connect
      --kind string         which forge the host runs, github or gitlab, when the host does not say
      --method string       how the credential comes: token from stdin, or github-app from the App in config (default "token")
      --token-file string   read the token from this file instead of stdin
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave forge](weave_forge.md)	 - Connect weave to the services pull requests live on

