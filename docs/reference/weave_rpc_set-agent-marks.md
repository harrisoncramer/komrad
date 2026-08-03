## weave rpc set-agent-marks

Put one mark on a list of agents, keyed by their ids, or clear the ones they carry by omitting the mark.

### Synopsis

Put one mark on a list of agents, keyed by their ids, or clear the ones they carry by omitting the mark. Every id given takes the same glyph, so marking agents differently is a call each. A mark is a glyph the user reads off the row, meaning whatever they decided it means, such as needs review or dealt with. Use only the glyphs get_config returns under marks, since the panel's mark key cycles through those and can only clear anything else. Archived agents have no row to draw a mark on and are skipped, and returns not_found only when no id given reached a live agent. What an agent is currently doing is already in its state, so do not mark that.

```
weave rpc set-agent-marks [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for set-agent-marks
      --ids strings           string, required, repeatable
      --mark string           string
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

