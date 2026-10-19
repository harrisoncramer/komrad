# Style guide

This guide covers everything written in komrad that a person or an agent reads: proto comments, docs pages, pull request descriptions, commit messages, RFCs, task notes, mail and review replies. The glossary and the general rules apply to all of it. The proto rules apply to the comments in `proto/proto`, which `mise run gen:docs` compiles into the API reference.

## General rules

Write short, plain sentences. Give each sentence one idea, and keep it under about 25 words.

Use the active voice and the present tense. Write "is" and "has" rather than "serves as" or "features".

Name the thing. A sentence that opens with "it" or "this" makes the reader look back for what it means.

Say what is true and stop. Leave out why a thing matters, what it reflects, and what it makes possible, unless the reader needs that to act.

Avoid the patterns that mark generated text. These include trailing "-ing" clauses that add significance ("ensuring that agents stay in sync"), negative parallels ("not just X, but Y"), lists padded to three, and vague attributions ("some callers find"). Do not use em dashes, bold, emoji or title case.

Put names from code in backticks, such as `notes_updated_at` or `get_task`.

## Proto comments

Write each comment for a reader who has the reference page open and has never seen the source. A comment says what the thing is or does from the caller's side. How komrad does it belongs in Go.

The API serves any client. Describe what a field holds or a call does, never how the panel or another client shows it.

### Services, messages and enums

Start with the name, then say what it is in one sentence: "Task is one piece of work that agents do." Add a second sentence only for something every reader needs.

### Fields

Open with the shape that fits the type. A value is a noun phrase, such as "The task's goal, in markdown." A bool opens with "Whether", a count with "How many", and a timestamp with "When" and ends "in RFC 3339". A request field naming a record says which record and how: "The task to archive, by id or slug."

Then say what an empty or unset value means, if it means anything. Two sentences at most. Behaviour the call performs belongs in the method description, not on its fields.

Name the owner rather than saying "its". Write "The branch of the task's checkout", not "The branch its checkout is on". The reference shows fields in a table, so never point at another field by position.

Do not restate validation. The reference prints each field's rules beside its comment.

Every field needs a comment, even an obvious one. Use the plain form for those. An `id` on a record is "The task's id." A `created_at` is "When the task was created, in RFC 3339." An `updated_at` is "When the task last changed, in RFC 3339." An `archived_at` is "When the task was archived, in RFC 3339. Unset while the task is live."

### Enum values

Say what the state means in one phrase, such as "A rebase stopped on a commit it could not replay." The reference leaves out the zero value, so its comment is for the source only.

### Methods

The `description` in `annotation.v1.surfaces` is the method's reference text, its CLI help, and its MCP tool description. Its first sentence is the summary, so start with an imperative verb and say what the call does: "Archive a task, keeping its record." Then add the effects a caller cannot see from the fields.

Advice to a model, such as when not to call the method or what to do on an error, goes in `mcp_guidance`. Only the MCP tool shows it.

## Glossary

Use one term for each idea, every time. A reader who meets two words assumes two things. Each entry gives the term, what it means, and the words it replaces.

### People and programs

person: A human using komrad. On a team, each person has an engine of their own. Not "the user", "you", or "a human".

agent: One CLI conversation that komrad runs in a pane and tracks.

CLI: The coding agent program an agent runs, such as Claude Code or Codex.

harness: The short name that picks a CLI, such as `claude` or `codex`. Use it for the field and the value. Use CLI for the program itself.

caller: Whoever makes the request, a person through any client, or an agent through MCP.

client: Any program that calls the API, such as the panel, the CLI, an MCP server or a script.

komrad: The system as a whole, and the subject of any sentence about what the server does. Not "the server" unless the process itself matters, as in its log or its config.

panel: komrad's terminal interface, one client among others. Use it only in writing about the interface itself. Proto comments never mention it.

embedded Postgres: The Postgres a release binary carries, which komrad runs under its state directory. Not "komrad's Postgres" or "the Postgres komrad runs".

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

parent: The task whose branch a task's branch starts from in place of the base branch. A task depends on its parent. Not "the task it stands on".

child: A task that has a parent.

stack: A chain of tasks, each the child of the one below it, from the bottom task on the base branch to the top task.

fork point: The commit of the parent's branch that a child's own commits sit on. Rounds and line counts measure from it. Not "tip" or "stand tip".

cascade: The pass in which komrad rebases each child onto its parent's newest commit, or onto the base branch once the parent merges. Not "restack".

orphaned: A child whose branch holds neither its fork point nor its parent's newest commit, so komrad cannot tell where its own commits begin. Not "lost".

re-target: To rebase a child onto the base branch, so it no longer has a parent. Not "move onto base".

origin: The git remote a worktree pushes to.

landed: Contained in full in the base branch. Use it for commits and rounds.

sequence: A git operation stopped part way, such as a rebase or merge with conflicts.

### Forges

forge: The service a repository's pull requests live on, such as GitHub or GitLab.

pull request: A request to merge a branch on a forge. It includes GitLab merge requests. Not "PR" or "MR".

forge proxy: The path git and `gh` on a remote engine take to the forge. The control plane adds the person's GitHub login.

### Engines

engine: A komrad server, its records and the machine its agents run on, either a laptop or a remote machine.

operator: A person who runs komrad's remote infrastructure. Operators grant access, add engines and deploy. Not "the engine's owner".

control plane: The machine in front of every remote engine. It routes each request to its engine, holds people's GitHub logins, and starts and stops engines. No engine runs on it.

### Reviews

round: A snapshot of the writer's worktree, saved as a commit, that a review is written against. A round never changes. Not "chapter" in the reference.

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

hidden: Live, but left out of a client's lists. Use it only for tasks, and never for archived.

deleted: Removed for good, with its transcript.

### Messages and records

mail: A message in an agent's mailbox. Use it for everything komrad delivers to an agent, including review notices. Not "doorbell".

event: Something komrad tells an agent about through mail, such as `pull_request.merged`.

hook: A script the person writes that komrad runs at a moment in a task's or agent's life.

CLI hook: A hook the CLI itself runs, which komrad reads to learn what an agent is doing. Always say "CLI hook" for these, never "hook" alone.

run: One execution of a hook, with its log, or one time a schedule's cron expression matched, with its state. Say which kind when the field does not. Not "firing".

schedule: A cron expression that starts a task or mails an orchestrator each time it matches.

document: Markdown an agent writes for the person to read.

transcript: An agent's whole conversation, as komrad keeps it.

### Values

empty: A string or list with nothing in it. Use it for fields without presence.

unset: An optional field or message field the caller did not set. Not "absent", "blank", or "missing".

RFC 3339: The format of every timestamp in a string field. Say it on each such field, as "in RFC 3339". A google.protobuf.Timestamp field needs no format note.

## Words not to use

Internal: capture-pane, doorbell, remover, row, stamp, store.

Position: above, below, earlier, the field before.

Vague: it (when the field is the first thing the reader sees), handle, thing, stuff.

Inflated: crucial, pivotal, robust, seamless, comprehensive, leverage, delve, ensure, enhance, showcase, underscore, additionally, notably, essentially.

Instead of is: serves as, acts as, functions as, stands as. Write "is".
