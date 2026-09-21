## weave tmux

Bind the panel key on your tmux server

### Synopsis

Bind the panel key on your tmux server.

This is what a tmux config runs, as `run-shell 'weave tmux'`, and it is where the
key that opens and hides the panel comes from. The path of this binary is resolved
once and written into the binding, so weave only has to be findable when tmux parses
its config rather than for the life of the server.

There is one key however many engines this machine knows: pressed, it opens this
machine's panel, or a picker when engine files are present. An engine has no key of
its own, so --engine is refused here.

A tmux server has to be running, since binding a key does not start one. The binding
belongs to the server rather than to a session, so it outlives the session it was
made from and dies with the server. Running this again binds the key as it now
stands and unbinds the one it bound before, so run it by hand after changing
@weave_key rather than restarting tmux, and after building weave somewhere else.

Run from a pane on weave's own server, it binds on the server the last open of the
panel came from, so a rebuild from inside the panel reaches your key too.

With --if-running it says nothing and does nothing when no tmux server is up, which
is what a build script wants.

```
weave tmux [flags]
```

### Options

```
  -h, --help         help for tmux
      --if-running   bind only when a tmux server is running, and say nothing when none is
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

