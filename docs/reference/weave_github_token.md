## weave github token

Print an installation token, minting one if the cached one is spent

### Synopsis

Print an installation token.

Run this to check the App is set up. A token starting ghs_ means the key, the App id
and the installation id all agree. Any other answer says which one does not.

The stand-in weave puts ahead of an agent's PATH as gh runs this for every call, so
a tool weave hands the token to never holds a stale one. --checkout is how that
stand-in says which tree it was run in, and refuses a tree whose origin is on a host
the App does not reach. --if-configured prints nothing and succeeds on a machine
that names no App, which leaves gh the credential the machine was logged in with.

```
weave github token [flags]
```

### Options

```
      --checkout string   refuse unless this checkout's origin is a repository on github.com
  -h, --help              help for token
      --if-configured     print nothing and succeed when the config names no app
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave github](weave_github.md)	 - Reach GitHub as the engine's own App

