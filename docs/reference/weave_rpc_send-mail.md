## weave rpc send-mail

Send one piece of mail to another agent, addressed by its id or by its name.

### Synopsis

Send one piece of mail to another agent, addressed by its id or by its name. An id is taken as it stands, so an id no agent holds is not_found rather than being tried as a name. A name is looked for among the live agents of your own workflow first, then of every workflow, where one that more than one workflow holds is refused as invalid_argument listing each holder with its id and workflow, so address that one by id. Nothing is delivered to a pane: the mail waits in the panel until the recipient reads its mail, so the recipient will not see it while it is busy and may not see it for a while. Use this to ask an agent working alongside you something only it knows, or to hand it a result it is waiting on, and say who you are and what you need in the mail itself. Set replyTo to the id of the mail you are answering so the two read as a thread. There is a limit on how much mail you and one other agent may exchange, counted in both directions over every thread, so opening a new subject is not a way around it and a send past the limit is refused for good. Wind an exchange up with a clear answer or a decision rather than another round once it feels like it is dragging, since the limit is there to be nowhere near rather than to be spent. Each call sends another piece of mail, so do not retry a call that may have succeeded, and do not send to yourself, which is refused. Returns not_found when no agent answers to the address, failed_precondition when the recipient is archived, and resource_exhausted when the recipient is already holding a full mailbox of unread mail or when you and the recipient have hit the limit on what one pair may exchange. A child agent may only mail its parent and its parent's other children; anything else is refused as permission_denied, so reply to your parent rather than looking for another way around.

```
weave rpc send-mail [flags]
```

### Options

```
      --body string           string, required, at most 10000 characters
      --compact               print the response on one line
  -h, --help                  help for send-mail
      --reply-to string       string
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
      --to string             string, required
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

