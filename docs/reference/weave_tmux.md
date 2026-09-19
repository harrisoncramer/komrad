## weave tmux

Bind the panel key on your tmux server

### Synopsis

Bind the panel key on your tmux server.

This is what a tmux config runs, as `run-shell 'weave tmux'`, and it is where the
key that opens and hides the panel comes from. The path of this binary is resolved
once and written into the binding, so weave only has to be findable when tmux parses
its config rather than for the life of the server.

A tmux server has to be running, since binding a key does not start one. The binding
belongs to the server rather than to a session, so it outlives the session it was
made from and dies with the server. Running this again binds the key as it now
stands and unbinds the one it bound before, so run it by hand after changing
@weave_key rather than restarting tmux.

```
weave tmux [flags]
```

### Options

```
  -h, --help   help for tmux
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

