## weave shown

Say in the exit status whether the panel is on screen on this tmux server

### Synopsis

Say whether the panel is on screen on this tmux server.

The panel key runs this to decide which of its two branches to take, so it is asked
on every press and answers from what is true at that moment rather than from
anything a previous press left behind. It succeeds when the panel has a client
attached and this is the server that opened it, and fails otherwise, which is what
sends the key to `weave open`.

Failing is the safe direction: a press that cannot get an answer opens the panel
rather than refusing to.

```
weave shown [flags]
```

### Options

```
  -h, --help   help for shown
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

