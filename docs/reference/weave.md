## weave

tmux panel and CLI for background coding agents

### Synopsis

Weave runs coding agents in tmux panes and keeps track of them.

There are two processes. `weave serve` is the long-lived service that owns the
database, answers the API and the MCP endpoint, and watches every pane. The panel
is a client of it, which is why it can be closed and reopened without stopping
anything. Every other command here is a client of the same service.

The panel is normally reached through the tmux key binding rather than by name,
and the commands below are for the times something has to be done by hand: check
the machine, start or stop the service, or call an endpoint from a script.

### Options

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
  -h, --help                 help for weave
```

### SEE ALSO

* [weave attach](weave_attach.md)	 - Attach the running panel in this terminal
* [weave doctor](weave_doctor.md)	 - Check that this machine has everything weave needs
* [weave engine](weave_engine.md)	 - Set up the engines that share a database
* [weave forge](weave_forge.md)	 - Connect weave to the services pull requests live on
* [weave github](weave_github.md)	 - Reach GitHub as the engine's own App
* [weave hide](weave_hide.md)	 - Hide the panel popup without stopping anything
* [weave hook](weave_hook.md)	 - Run one of the agent CLI hooks weave installs
* [weave log](weave_log.md)	 - Show the log lines the service wrote under one trace id
* [weave migrate](weave_migrate.md)	 - Apply any pending database migrations
* [weave open](weave_open.md)	 - Open the panel popup, creating or attaching its persistent session
* [weave panel](weave_panel.md)	 - Run the panel itself, as the manager session's own command
* [weave pr](weave_pr.md)	 - Read pull requests through the forges weave is connected to
* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC
* [weave serve](weave_serve.md)	 - Run the weave service in the foreground
* [weave shown](weave_shown.md)	 - Say in the exit status whether the panel is on screen on this tmux server
* [weave start](weave_start.md)	 - Start the service in the background if it is not already running
* [weave stop](weave_stop.md)	 - Ask the running service to stop, and wait until it is gone
* [weave tmux](weave_tmux.md)	 - Bind the panel key on your tmux server
* [weave version](weave_version.md)	 - Print which build this binary is, and which one is serving

