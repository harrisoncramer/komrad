## weave serve

Run the weave service in the foreground

### Synopsis

Run the weave service in the foreground.

It opens the database, mounts the Connect handlers and the MCP endpoint on one
address, and runs the loops that read each pane's state, reconcile schedules, and
sync transcripts. The panel and the rest of these commands are clients of it, so
nothing works without it.

`weave start` spawns this detached with its output going to the log file, which is
how it usually runs. Run it here to watch that output directly. It stops on an
interrupt, or when `weave stop` asks it to.

```
weave serve [flags]
```

### Options

```
      --addr string   listen address, defaulting to WEAVE_ADDR
  -h, --help          help for serve
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

