## weave github credential

Answer git's credential helper protocol with an installation token

### Synopsis

Answer git when it asks for a password.

Git runs this and describes what it wants to reach. This answers only for https to
github.com. Anything else gets no answer, so git falls back to whatever else the
account has configured.

Nothing is stored. The token is made when git asks and stops working within the hour.

Set it up with:

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

