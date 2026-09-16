## weave github

Reach GitHub as the engine's own App

### Synopsis

Reach GitHub as the engine's own App.

An engine runs agents that write code, so it has to push. Giving it a personal
access token would leave a secret on the machine that works forever.

Instead weave holds a GitHub App's private key and signs a short assertion with it.
GitHub returns a token that stops working after an hour and only reaches the
repositories the App was installed on.

Set the App id, the installation id and the path to the key under `[github_app]`.

### Options

```
  -h, --help   help for github
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents
* [weave github credential](weave_github_credential.md)	 - Answer git's credential helper protocol with an installation token
* [weave github token](weave_github_token.md)	 - Print an installation token, minting one if the cached one is spent

