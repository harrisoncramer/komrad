## weave stop

Ask the running service to stop, and wait until it is gone

### Synopsis

Ask the running service to stop, and wait until it is gone.

This asks rather than signals, so the service can finish the launch it is in the
middle of and say when it is done. It returns once the process has actually gone,
not when the address stops answering, which happens well before the shutdown is
finished. Nothing serving at the address is not an error.

Agents keep running: they are tmux panes, not children of this process. They are
picked back up when the service returns.

On a machine where the service is a systemd unit this refuses, since a service
stopped this way stays down until systemctl starts it again.

```
weave stop [flags]
```

### Options

```
  -h, --help   help for stop
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

