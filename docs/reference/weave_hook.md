## weave hook

Run one of the agent CLI hooks weave installs

### Synopsis

Run one of the agent CLI hooks weave installs.

An agent CLI runs these itself at a point in its own loop. They read the hook
payload on stdin and write whatever that CLI expects on stdout. Nothing here is
meant to be run by hand.

### Options

```
  -h, --help   help for hook
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents
* [weave hook permission-request](weave_hook_permission-request.md)	 - Record the tool an agent is waiting to be allowed to run
* [weave hook post-tool-use](weave_hook_post-tool-use.md)	 - Record that an agent's tool call returned
* [weave hook pre-tool-use](weave_hook_pre-tool-use.md)	 - Record the tool an agent is about to run
* [weave hook stop](weave_hook_stop.md)	 - Deliver an agent's unread mail as its turn ends
* [weave hook turn-end](weave_hook_turn-end.md)	 - Record that an agent's turn ended

