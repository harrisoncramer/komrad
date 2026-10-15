# Style guide

This guide covers the comments in `proto/proto`, which `mise run gen:docs` compiles into the API reference. Write each one for a reader who has the reference page open and has never seen the source.

## Glossary

Use one term for each idea, every time. A reader who meets two words assumes two things. Each entry gives the term, what it means, and the words it replaces.

### People and programs

person: The one human running komrad. Not "the user", "you", or "a human".

agent: One CLI conversation that komrad runs in a pane and tracks.

CLI: The coding agent program an agent runs, such as Claude Code or Codex.

harness: The short name that picks a CLI, such as `claude` or `codex`. Use it for the field and the value. Use CLI for the program itself.

caller: Whoever makes the request, a person through the panel or CLI, or an agent through MCP.

komrad: The system as a whole, and the subject of any sentence about what the server does. Not "the server" unless the process itself matters, as in its log or its config.

panel: The terminal interface the person uses. Use it only for what is drawn or pressed. Not "the sidebar" unless the sidebar itself is meant.

### Roles

writer: The one agent on a task that changes its checkout, for the whole life of the task.

reviewer: An agent on a task that reads the writer's change and gives a review. It makes no changes of its own.

orchestrator: The one agent per workflow that hands work out across its tasks.

### Work

workflow: A group of tasks that share a directory, flags, harness and context.

task: One piece of work. It holds the checkout, branch, pull request, reviews and notes, and outlives its agents.

goal: What a task is meant to achieve, in markdown.

notes: The markdown page on a task that says where its work stands.

slug: The short readable name of a task, such as `fix-login`. No two tasks ever share one.

id: The opaque string that every other call uses to name a record. Not "handle".

prompt: The text an agent starts with. Not "briefing", "opening briefing", or "opening prompt".

context: Text a workflow adds to the system prompt of every agent it launches.

flags: The command-line flags a CLI starts with.

### Directories and git

worktree: A git worktree komrad creates for one task, on a branch named `task/<task id>`. Not "cut", "provisioned directory", or "directory komrad cut".

checkout: The directory an agent works in. This is the task's worktree, or the workflow's directory when the workflow creates no worktrees.

base branch: The branch worktrees are created from and changes are measured against. Not "the base" on its own.

origin: The git remote a worktree pushes to.

landed: Held in full by the base branch. Use it for commits and rounds.

sequence: A git operation stopped part way, such as a rebase or merge with conflicts.

### Forges

forge: The service a repository's pull requests live on, such as GitHub or GitLab.

pull request: A request to merge a branch on a forge. It includes GitLab merge requests. Not "PR" or "MR".

### Reviews

round: A snapshot of the writer's worktree, stored as a commit, that a review is written against. A round never changes. Not "chapter" in the reference.

review: One author's comments on one round, sent to the agent together.

thread: A conversation pinned to a range of lines in a round.

comment: One entry in a thread.

verdict: What a review concludes: approved, changes requested, or commented.

released: Sent to the agent. Use it for reviews and comments. Not "let go of".

resolved: Marked by its author as needing nothing more. Use it for threads. Not "settled".

### Agent lifecycle

create: Make a new agent and launch it. Not "spawn".

launch: Start a CLI in a new pane.

dormant: Kept as a record with no pane. A dormant agent keeps its conversation.

resume: Launch a dormant agent on its existing conversation.

restart: Launch the writer on a new conversation.

retry: Launch an agent again after its create failed.

held: Interrupted and kept still by a caller, such as before editing files in its checkout. Release lets it go on.

archived: Stopped and taken out of the list, with its records kept. Restore brings it back, dormant. Not "hidden from the panel" or "put away".

hidden: Left out of the panel's sidebar while still live. Use it only for tasks, and never for archived.

deleted: Removed for good, with its transcript.

### Messages and records

mail: A message in an agent's mailbox. Use it for everything komrad delivers to an agent, including review notices. Not "doorbell".

event: Something komrad tells an agent about through mail, such as `pull_request.merged`.

hook: A script the person writes that komrad runs at a moment in a task's or agent's life.

CLI hook: A hook the CLI itself fires, which komrad reads to learn what an agent is doing. Always say "CLI hook" for these, never "hook" alone.

run: One time komrad ran a hook, with its log.

schedule: A cron expression that starts a task or mails an orchestrator each time it matches.

document: Markdown an agent writes for the person to read.

transcript: An agent's whole conversation, as komrad stored it.

### Values

empty: A string or list with nothing in it. Use it for fields without presence.

unset: An optional field or message field the caller did not set. Not "absent", "blank", or "missing".

RFC 3339: The format of every timestamp string. Say it on every timestamp field, as "in RFC 3339".

## Words not to use

These words either name something internal, point at a place that does not exist on the page, or read like generated text.

Internal: capture-pane, doorbell, remover, row, stamp, store.

Position: above, below, earlier, the field before.

Vague: it (when the field is the first thing the reader sees), handle, thing, stuff.

Inflated: crucial, pivotal, robust, seamless, comprehensive, leverage, delve, ensure, enhance, showcase, underscore, additionally, notably, essentially.

Copula avoidance: serves as, acts as, functions as, stands as. Write "is".
