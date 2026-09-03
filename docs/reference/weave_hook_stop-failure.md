## weave hook stop-failure

Record that an agent's turn ended in an API error

### Synopsis

Record that an agent's turn ended in an API error.

The agent CLI runs this on its StopFailure event, which is the only turn end it
reports when a turn dies rather than finishes. Weave keeps the moment against the
agent so the panel and the delivery loop see the turn end, and writes why it died
into the agent's log. Nothing is printed back, since a dead turn takes no
continuation, so any unread mail is left for the panel to nudge the agent about.

```
weave hook stop-failure [flags]
```

### Options

```
  -h, --help   help for stop-failure
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave hook](weave_hook.md)	 - Run one of the agent CLI hooks weave installs

