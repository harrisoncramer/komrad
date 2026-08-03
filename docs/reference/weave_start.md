## weave start

Start the service in the background if it is not already running

### Synopsis

Start the service in the background if it is not already running.

It asks the configured address who is there first. A server of this build is left
alone; one of another build is asked to stop and replaced, so a rebuilt binary is
the one answering. Otherwise `weave serve` is spawned detached, with its output
going to the log file, and this waits for the address to answer before returning.

Opening the panel does the same thing on its own, so this is for scripts and for
bringing the service up without a panel.

```
weave start [flags]
```

### Options

```
  -h, --help   help for start
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

