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
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents
* [weave rpc add-review-message](weave_rpc_add-review-message.md)	 - Say something in a review thread, which is how you answer feedback on your own diff.
* [weave rpc adopt](weave_rpc_adopt.md)	 - Put an agent on work that already exists, naming the branch it is on.
* [weave rpc archive-agents](weave_rpc_archive-agents.md)	 - Stop a list of agents and hide them from the panel, keyed by id, keeping their records and transcripts.
* [weave rpc archive-schedule](weave_rpc_archive-schedule.md)	 - Hide a schedule from the panel so it stops firing, keeping its record.
* [weave rpc archive-workflow](weave_rpc_archive-workflow.md)	 - Hide a workflow from the panel, archiving the agents inside it.
* [weave rpc create-agent](weave_rpc_create-agent.md)	 - Launch a background agent with an opening prompt.
* [weave rpc create-review-thread](weave_rpc_create-review-thread.md)	 - Pin a comment to a place in another agent's diff, which is how you give it a review rather than mailing it prose.
* [weave rpc create-schedule](weave_rpc_create-schedule.md)	 - Create a recurring agent launch inside a workflow.
* [weave rpc create-workflow](weave_rpc_create-workflow.md)	 - Create a workflow to group agents under.
* [weave rpc delete-agents](weave_rpc_delete-agents.md)	 - Permanently remove a list of agents, keyed by id.
* [weave rpc delete-mail](weave_rpc_delete-mail.md)	 - Permanently remove a list of mail, keyed by id.
* [weave rpc delete-workflow](weave_rpc_delete-workflow.md)	 - Permanently delete an archived workflow along with whatever archived agents are still inside it, reusing the same worktree-removal handling a direct agent delete uses.
* [weave rpc empty-archive](weave_rpc_empty-archive.md)	 - Permanently delete every archived agent and workflow, tearing down each worktree that is still on disk.
* [weave rpc get-agent](weave_rpc_get-agent.md)	 - Fetch one agent by id, including whatever metadata it has recorded about its own work.
* [weave rpc get-config](weave_rpc_get-config.md)	 - Read the panel's configuration as it is currently in force, keyed the same way the user's config file is.
* [weave rpc get-log-lines](weave_rpc_get-log-lines.md)	 - Read every log line the service wrote under one trace id, in order.
* [weave rpc get-schedule](weave_rpc_get-schedule.md)	 - Fetch one schedule by id.
* [weave rpc get-transcript](weave_rpc_get-transcript.md)	 - Fetch one window of an agent's transcript by agent id, not by name.
* [weave rpc get-workflow](weave_rpc_get-workflow.md)	 - Fetch one workflow by id.
* [weave rpc hold-agent](weave_rpc_hold-agent.md)	 - Stop an agent and hold it, which is what somebody does before editing the files that agent is working in.
* [weave rpc list-agents](weave_rpc_list-agents.md)	 - List the agents the panel tracks, live and dormant, with each one's name, state, working directory, flags, and workflow.
* [weave rpc list-hooks](weave_rpc_list-hooks.md)	 - List the hooks the user has written, which are executable files in their hooks directory.
* [weave rpc list-mail](weave_rpc_list-mail.md)	 - List the mail addressed to an agent, most recent first, without claiming any of it: unlike read_mail this never empties the mailbox.
* [weave rpc list-recent-traces](weave_rpc_list-recent-traces.md)	 - List the traces the service's log ends with, oldest first, each with when it started, what it was about and how many lines it holds.
* [weave rpc list-review-rounds](weave_rpc_list-review-rounds.md)	 - Read the rounds taken of an agent, oldest first.
* [weave rpc list-review-threads](weave_rpc_list-review-threads.md)	 - Read the review conversations pinned to the lines of a diff.
* [weave rpc list-reviews](weave_rpc_list-reviews.md)	 - Read every review released on an agent, in the order their rounds were taken, each with the verdict it carried.
* [weave rpc list-schedules](weave_rpc_list-schedules.md)	 - List the recurring agent launches, most recently updated first, with each one's cron expression, prompt, workflow, next run time, and the outcome of its last firing.
* [weave rpc list-workflows](weave_rpc_list-workflows.md)	 - List the workflows agents are grouped into, in alphabetical order, with each one's shared context, the working directory and flags its agents start from, and the scripts hooked to moments in their agents' lives.
* [weave rpc log](weave_rpc_log.md)	 - Write one line to weave's log, tagged to an agent.
* [weave rpc open-review-round](weave_rpc_open-review-round.md)	 - Freeze what an agent has written right now and get back the round to review against, which is a snapshot commit covering its whole worktree, committed and uncommitted alike.
* [weave rpc plan-rebase](weave_rpc_plan-rebase.md)	 - Read what rebasing an agent's worktree onto its base would do, without moving anything.
* [weave rpc read-mail](weave_rpc_read-mail.md)	 - Take the unread mail other agents have sent you, oldest first.
* [weave rpc rebase-worktree](weave_rpc_rebase-worktree.md)	 - Move an agent's branch onto the latest commit of its base, rewriting the branch in the checkout it sits in.
* [weave rpc release-agent](weave_rpc_release-agent.md)	 - Let an agent that hold_agent stopped carry on, telling it what happened while it was held.
* [weave rpc release-review](weave_rpc_release-review.md)	 - Let go of the review you have been writing on another agent's diff, which is what sends it.
* [weave rpc rename-agent](weave_rpc_rename-agent.md)	 - Rename an agent, keyed by its id rather than its current name.
* [weave rpc request-review](weave_rpc_request-review.md)	 - Get your own work read before you call it finished, which spawns the reviewer for you and hands you back the rest of the loop.
* [weave rpc resolve-review-thread](weave_rpc_resolve-review-thread.md)	 - Settle a review thread you wrote, once the answer to it is good enough, or reopen one you settled too early.
* [weave rpc restore-agents](weave_rpc_restore-agents.md)	 - Take a list of agents out of the archive, keyed by id, along with the workflows they belong to.
* [weave rpc restore-workflow](weave_rpc_restore-workflow.md)	 - Take a workflow out of the archive, keyed by id, along with every agent archived inside it.
* [weave rpc resume-agent](weave_rpc_resume-agent.md)	 - Relaunch an agent's CLI on the same conversation, keyed by id, which is what reaches an agent whose pane has since died.
* [weave rpc retry-agent](weave_rpc_retry-agent.md)	 - Retry launching an agent that failed to create, keyed by id, optionally with corrected flags, prompt, or harness.
* [weave rpc search-transcripts](weave_rpc_search-transcripts.md)	 - Full-text search over conversations, including archived ones, returning one hit per matching turn with the matched text marked.
* [weave rpc send-mail](weave_rpc_send-mail.md)	 - Send one piece of mail to another agent, addressed by its id.
* [weave rpc set-agent-hidden](weave_rpc_set-agent-hidden.md)	 - Hide an agent from the panel's sidebar, keyed by its id, or unhide it by passing hidden false.
* [weave rpc set-agent-metadata](weave_rpc_set-agent-metadata.md)	 - Record one fact about an agent's work under a key of your choosing, keyed by the agent's id, or clear that key by omitting the value.
* [weave rpc set-mark](weave_rpc_set-mark.md)	 - Put a single lowercase letter on an agent's row or a workflow's row, which is the shorthand a person types in the panel to jump straight to that row.
* [weave rpc set-schedule-enabled](weave_rpc_set-schedule-enabled.md)	 - Pause or resume one schedule without editing anything else.
* [weave rpc start-orchestrator](weave_rpc_start-orchestrator.md)	 - Bring a workflow's orchestrator back with a pane to talk to, starting the one that workflow has never had.
* [weave rpc update-schedule](weave_rpc_update-schedule.md)	 - Replace a schedule's name, cron expression, timezone, prompt, workflow, and whether the agents it launches start hidden from the panel.
* [weave rpc update-workflow](weave_rpc_update-workflow.md)	 - Replace a workflow's name, context, directory, flags, worktree config and hooks.

