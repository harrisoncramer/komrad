## weave github token

Print an installation token, minting one if the cached one is spent

### Synopsis

Print an installation token.

Run this to check the App is set up. A token starting ghs_ means the key, the App id
and the installation id all agree. Any other answer says which one does not.

```
weave github token [flags]
```

### Options

```
  -h, --help   help for token
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave github](weave_github.md)	 - Reach GitHub as the engine's own App

