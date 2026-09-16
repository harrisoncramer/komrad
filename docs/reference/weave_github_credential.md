## weave github credential

Answer git's credential helper protocol with an installation token

### Synopsis

Answer git's credential helper protocol.

Git runs this with an action and a description of what it wants to reach. Only
`get` is answered, and only for github.com, so anything else falls through to
whatever else the account has configured. Nothing is stored, because the token this
returns is minted on demand and expires within the hour.

Wire it up with:

    git config --global credential.https://github.com.helper '!weave github credential'

```
weave github credential [flags]
```

### Options

```
  -h, --help   help for credential
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave github](weave_github.md)	 - Reach GitHub as the engine's own App

