## weave forge

Connect weave to the services pull requests live on

### Synopsis

Connect weave to the services pull requests live on.

Weave reads the pull requests its agents open through a connection to each forge,
made once and kept until it is disconnected. The service holds the connection: a
token lives in a file under its state directory that only its user can read, and
the row in its database records who the forge said the credential belongs to.
Every command here talks to the service, so with --engine it connects the engine.

### Options

```
  -h, --help   help for forge
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents
* [weave forge check](weave_forge_check.md)	 - Ask every connected forge whether its credential still works
* [weave forge connect](weave_forge_connect.md)	 - Connect a forge with a token read from stdin, or as the engine's GitHub App
* [weave forge disconnect](weave_forge_disconnect.md)	 - Forget a forge and the token it was connected with
* [weave forge list](weave_forge_list.md)	 - Show every connected forge and who weave acts as there

