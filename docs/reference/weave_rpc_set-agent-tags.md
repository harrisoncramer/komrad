## weave rpc set-agent-tags

Put one tag on a list of agents, keyed by their ids, or clear the ones they carry by omitting the tag.

### Synopsis

Put one tag on a list of agents, keyed by their ids, or clear the ones they carry by omitting the tag. Every id given takes the same glyph, so tagging agents differently is a call each. A tag is a glyph the user reads off the row, meaning whatever they decided it means, such as needs review or dealt with. Use only the glyphs get_config returns under tags, since the panel's tag key cycles through those and can only clear anything else. Archived agents have no row to draw a tag on and are skipped, and returns not_found only when no id given reached a live agent. What an agent is currently doing is already in its state, so do not tag that.

```
weave rpc set-agent-tags [flags]
```

### Options

```
      --compact               print the response on one line
  -h, --help                  help for set-agent-tags
      --ids strings           string, required, repeatable
      --request-json string   the whole request as JSON, which the flags above are laid over
      --tag string            string
      --timeout duration      how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

