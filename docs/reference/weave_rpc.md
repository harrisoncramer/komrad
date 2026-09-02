## weave rpc

Call the service's endpoints, one subcommand per RPC

### Synopsis

Call the service's endpoints from a script.

Every subcommand here is one RPC, named as the MCP tool surface names it, so a
script and a model ask for the same operation by the same name. Request fields are
flags, and the response is the API's own JSON on stdout.

### Options

```
  -h, --help   help for rpc
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents
* [weave rpc add-review-message](weave_rpc_add-review-message.md)	 - Say something in a review thread, which is how you answer feedback on your own diff.
* [weave rpc adopt](weave_rpc_adopt.md)	 - Put an agent on work that already exists, naming a pull request by URL or number or naming a branch.
* [weave rpc archive-agents](weave_rpc_archive-agents.md)	 - Stop a list of agents and hide them from the panel, keyed by id, keeping their records and transcripts.
* [weave rpc archive-schedule](weave_rpc_archive-schedule.md)	 - Hide a schedule from the panel so it stops firing, keeping its record.
* [weave rpc archive-workflow](weave_rpc_archive-workflow.md)	 - Hide a workflow from the panel, archiving the agents inside it.
* [weave rpc create-agent](weave_rpc_create-agent.md)	 - Launch a background agent with an opening prompt.
* [weave rpc create-schedule](weave_rpc_create-schedule.md)	 - Create a recurring agent launch inside a workflow.
* [weave rpc create-workflow](weave_rpc_create-workflow.md)	 - Create a workflow to group agents under.
* [weave rpc delete-agents](weave_rpc_delete-agents.md)	 - Permanently remove a list of agents, keyed by id.
* [weave rpc delete-mail](weave_rpc_delete-mail.md)	 - Permanently remove a list of mail, keyed by id.
* [weave rpc delete-workflow](weave_rpc_delete-workflow.md)	 - Permanently delete an archived workflow along with whatever archived agents are still inside it, reusing the same deprovision-action handling a direct agent delete uses.
* [weave rpc get-agent](weave_rpc_get-agent.md)	 - Fetch one agent by id, including whatever metadata it has recorded about its own work.
* [weave rpc get-config](weave_rpc_get-config.md)	 - Read the panel's configuration as it is currently in force, keyed the same way the user's config file is.
* [weave rpc get-schedule](weave_rpc_get-schedule.md)	 - Fetch one schedule by id.
* [weave rpc get-transcript](weave_rpc_get-transcript.md)	 - Fetch one agent's full transcript by agent id, not by name.
* [weave rpc get-workflow](weave_rpc_get-workflow.md)	 - Fetch one workflow by id.
* [weave rpc list-actions](weave_rpc_list-actions.md)	 - List the actions the user has written, which are executable files in their actions directory.
* [weave rpc list-agents](weave_rpc_list-agents.md)	 - List the agents the panel tracks, live and dormant, with each one's name, state, working directory, flags, and workflow.
* [weave rpc list-mail](weave_rpc_list-mail.md)	 - List the mail addressed to an agent, most recent first, without claiming any of it: unlike read_mail this never empties the mailbox.
* [weave rpc list-review-threads](weave_rpc_list-review-threads.md)	 - Read the review conversations pinned to the lines of your own diff.
* [weave rpc list-schedules](weave_rpc_list-schedules.md)	 - List the recurring agent launches, most recently updated first, with each one's cron expression, prompt, workflow, next run time, and the outcome of its last firing.
* [weave rpc list-workflows](weave_rpc_list-workflows.md)	 - List the workflows agents are grouped into, in alphabetical order, with each one's shared context, the working directory and flags its agents start from, and the action run before its agents start and the one run after they are deleted.
* [weave rpc log](weave_rpc_log.md)	 - Write one line to weave's log, tagged to an agent.
* [weave rpc read-mail](weave_rpc_read-mail.md)	 - Take the unread mail other agents have sent you, oldest first.
* [weave rpc rename-agent](weave_rpc_rename-agent.md)	 - Rename an agent, keyed by its id rather than its current name.
* [weave rpc restore-agents](weave_rpc_restore-agents.md)	 - Take a list of agents out of the archive, keyed by id, along with the workflows they belong to.
* [weave rpc restore-workflow](weave_rpc_restore-workflow.md)	 - Take a workflow out of the archive, keyed by id, along with every agent archived inside it.
* [weave rpc resume-agent](weave_rpc_resume-agent.md)	 - Relaunch an agent's CLI on the same conversation, keyed by id, which is what reaches an agent whose pane has since died.
* [weave rpc run-action](weave_rpc_run-action.md)	 - Fire one of the user's actions on an agent, naming the action as list_actions reports it and the agent by id.
* [weave rpc search-transcripts](weave_rpc_search-transcripts.md)	 - Full-text search over conversations, including archived ones, returning one hit per matching turn with the matched text marked.
* [weave rpc send-mail](weave_rpc_send-mail.md)	 - Send one piece of mail to another agent, addressed by its id.
* [weave rpc set-agent-hidden](weave_rpc_set-agent-hidden.md)	 - Hide an agent from the panel's sidebar, keyed by its id, or unhide it by passing hidden false.
* [weave rpc set-agent-metadata](weave_rpc_set-agent-metadata.md)	 - Record one fact about an agent's work under a key of your choosing, keyed by the agent's id, or clear that key by omitting the value.
* [weave rpc set-agent-tags](weave_rpc_set-agent-tags.md)	 - Put one tag on a list of agents, keyed by their ids, or clear the ones they carry by omitting the tag.
* [weave rpc set-schedule-enabled](weave_rpc_set-schedule-enabled.md)	 - Pause or resume one schedule without editing anything else.
* [weave rpc update-schedule](weave_rpc_update-schedule.md)	 - Replace a schedule's name, cron expression, timezone, prompt, workflow, and whether the agents it launches start hidden from the panel.
* [weave rpc update-workflow](weave_rpc_update-workflow.md)	 - Replace a workflow's name, context, directory, flags, and the provisioner that owns its agents' directories.

