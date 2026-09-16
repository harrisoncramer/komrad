## weave rpc list-mail

List the mail addressed to an agent, most recent first, without claiming any of it: unlike read_mail this never empties the mailbox.

### Synopsis

List the mail addressed to an agent, most recent first, without claiming any of it: unlike read_mail this never empties the mailbox. claimedAt is unset while a piece is still waiting, and set once the recipient has taken it. id defaults to your own, which is in the WEAVE_AGENT_ID environment variable; give another agent's id to look at its mailbox instead, which is a deliberate read of somebody else's mail and is recorded as one. Returns one page, pass the response's nextPageToken back as pagination.cursor for the next one. direction says which half of the correspondence to list, the mail addressed to the agent by default, or MAIL_DIRECTION_SENT for the mail it sent. Returns not_found if no agent has that id.

```
weave rpc list-mail [flags]
```

### Options

```
      --compact                    print the response on one line
      --direction string           MAIL_DIRECTION_RECEIVED|MAIL_DIRECTION_SENT
  -h, --help                       help for list-mail
      --id string                  string
      --pagination.cursor string   string, at most 2048 characters
      --pagination.limit int32     int32
      --request-json string        the whole request as JSON, which the flags above are laid over
      --timeout duration           how long to wait for the call (default 30s)
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

