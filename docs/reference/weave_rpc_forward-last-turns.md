## weave rpc forward-last-turns

Send another agent's newest turns to a third agent as mail weave composes itself, so what that agent concluded reaches the recipient without passing through your own context.

### Synopsis

Send another agent's newest turns to a third agent as mail weave composes itself, so what that agent concluded reaches the recipient without passing through your own context. Name the source as fromAgentId and the recipient as toAgentId, both by id. It forwards the source's last turn by default, or its newest turns when turns is set, with the prompts between them, each cut from the head to fit maxBytes so the conclusion survives, exactly as get_last_turns would return them. The body opens with a header naming the source, its branch, the turns forwarded and when they were read, then your note when you give one, then the turns; the recipient is told to read them as quoted material. The response says what was sent and how big it was, and never carries the text, since keeping it out of your context is the point. It carries a warning when the recipient is dormant, since nothing runs behind it and the mail waits until it is resumed. The mail is sent from you, so every rule of send_mail holds: it counts toward what you and the recipient may exchange, it is refused when the recipient is archived or its mailbox is full, and a child may only forward to its parent and its parent's other children, judged on you rather than on the source. Reading the source is recorded as a deliberate read of another agent's work, the way get_transcript is. Mail holds at most 10000 characters, and a forward that would run past that is refused as invalid_argument rather than cut, naming how long it came to; pass a smaller maxBytes or fewer turns and call again. Refused as failed_precondition when the source has no turns synced yet. Each call sends another piece of mail, so do not retry a call that may have succeeded. Use this when the recipient needs the text and you do not; use get_last_turns when you do.

```
weave rpc forward-last-turns [flags]
```

### Options

```
      --compact                print the response on one line
      --from-agent-id string   string, required
  -h, --help                   help for forward-last-turns
      --max-bytes int32        int32
      --note string            string, at most 10000 characters
      --request-json string    the whole request as JSON, which the flags above are laid over
      --timeout duration       how long to wait for the call (default 30s)
      --to-agent-id string     string, required
      --turns int32            int32, between 1 and 20
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

