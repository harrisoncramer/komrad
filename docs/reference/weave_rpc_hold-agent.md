## weave rpc hold-agent

Stop an agent and hold it, which is what somebody does before editing the files that agent is working in.

### Synopsis

Stop an agent and hold it, which is what somebody does before editing the files that agent is working in. The agent's terminal is interrupted and the hold is granted only once the agent is observed to have come to rest, so a turn part-way through is ended rather than waited out. Never call this on yourself: the interrupt would end the very turn that asked for it, leaving you stopped with nobody who knows to let you go, so your own id is refused as invalid_argument. A held agent runs nothing until release_agent lets it go, and weave refuses to rebase, archive or otherwise move an agent while it is held. An agent already held is refused as already_exists; one with no pane, one whose terminal could not be read, and one that would not settle within the budget are all refused as failed_precondition. Holding an agent stops work, so hold one because you were asked to and not to get it out of your way.

```
weave rpc hold-agent [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for hold-agent
      --id string             string, required
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

