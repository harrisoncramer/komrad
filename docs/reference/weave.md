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
  -h, --help                 help for weave
```

### SEE ALSO

* [weave attach](weave_attach.md)	 - Attach the running panel in this terminal
* [weave doctor](weave_doctor.md)	 - Check that this machine has everything weave needs
* [weave hide](weave_hide.md)	 - Hide the panel popup without stopping anything
* [weave hook](weave_hook.md)	 - Run one of the agent CLI hooks weave installs
* [weave migrate](weave_migrate.md)	 - Apply any pending database migrations
* [weave open](weave_open.md)	 - Open the panel popup, creating or attaching its persistent session
* [weave palette](weave_palette.md)	 - Open the command palette over the panel
* [weave panel](weave_panel.md)	 - Run the panel itself, as the manager session's own command
* [weave readonly-role](weave_readonly-role.md)	 - Create the least-privilege role the MCP query tools connect as
* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC
* [weave serve](weave_serve.md)	 - Run the weave service in the foreground
* [weave start](weave_start.md)	 - Start the service in the background if it is not already running
* [weave stop](weave_stop.md)	 - Ask the running service to stop, and wait until it is gone
* [weave tmux](weave_tmux.md)	 - Bind the panel key on your tmux server
* [weave version](weave_version.md)	 - Print which build this binary is, and which one is serving

