## weave rpc send-message

Send one message to another agent, addressed by its id.

### Synopsis

Send one message to another agent, addressed by its id. Names are not accepted, since two workflows may each hold an agent of the same name; call list_agents to turn a name into the id it stands for. Nothing is delivered to a pane: the message waits in the panel until the recipient reads its mail, so the recipient will not see it while it is busy and may not see it for a while. Use this to ask an agent working alongside you something only it knows, or to hand it a result it is waiting on, and say who you are and what you need in the message itself. Set replyTo to the id of the message you are answering so the two read as a thread, but a thread can only run so many hops before a send is refused, so wind it up with a clear answer or a decision rather than another round once it feels like it is dragging. Each call sends another message, so do not retry a call that may have succeeded, and do not send to yourself, which is refused. Returns not_found when no agent holds that id, failed_precondition when the recipient is archived, and resource_exhausted when the recipient is already holding a full mailbox of unread mail or when the thread has hit its hop limit.

```
weave rpc send-message [flags]
```

### Options

```
      --body string           string, required, at most 10000 characters
      --compact               print the response on one line
  -h, --help                  help for send-message
      --reply-to string       string
      --request-json string   the whole request as JSON, which the flags above are laid over
      --timeout duration      how long to wait for the call (default 30s)
      --to-agent-id string    string, required
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave rpc](weave_rpc.md)	 - Call the service's endpoints, one subcommand per RPC

