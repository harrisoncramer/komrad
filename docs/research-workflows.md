# Spike: workflows as a scoping resource over chats

Status: proposal. Nothing here is built. This is a sketch of how a "workflow"
concept could sit on top of the current session model without disturbing the
parts that already work.

## What a workflow is

A workflow is a bucket of related chats. Today every chat is a flat sibling of
every other chat. The root list shows all of them, search spans all of them, and
there is no way to say "these five chats are the same piece of work." A workflow
gives that grouping a name and a home.

The idea is to make a workflow a first-class resource in the same sense a chat
already is: a row in the database with an id, a name, and a soft-delete flag,
that the panel can list and act on. A chat then optionally belongs to one
workflow. The workflow view is a clone of the top-level view of the app, except
everything inside it is scoped to that one workflow.

## The current model, briefly

A chat is a row in the `sessions` table, defined in db/schema/session.hcl. It
carries its id, name, working directory, flags, opening prompt, the tmux pane it
runs in, its lifecycle state, and Claude's own conversation id for resuming. The
row outlives the tmux session, so a dormant chat can be restored later from its
stored cwd, flags, and conversation id.

The panel (internal/panel) is a bubbletea program with a handful of screens:
the root list, the chats picker, and search. The root list is three rows built
by `buildRows`: new chat, chats, and search. The chats picker lists every live
and dormant chat with a preview of the hovered pane. Search reads every chat's
transcript, including soft-deleted ones, and matches on content.

The serve process owns the store and runs the reconciler (internal/agents/
reconcile.go), which observes each chat's pane every couple of seconds and writes
the state that observation implies. No chat state is set anywhere else.

The cli loop (internal/cli/cli.go) is the glue. It opens the panel, and when the
user picks a chat it attaches to that chat's tmux session in place. When they
detach, it loops back into the panel with a `focus *store.Agent` so the panel
reopens on the chats picker with that chat selected, rather than at the root
list. If the chat's tmux session is gone, the loop restores it first.

## The proposed model

Add a `workflows` table that mirrors the shape of `sessions` where it matters:
an id (uuidv7 text, same scheme), a name, `is_deleted`, and the timestamps. It
does not need cwd, flags, pane, or state, because a workflow does not run
anything. It is pure organization.

Add one column to `sessions`: `workflow_id text null`. A null means the chat
belongs to no workflow. Null, not empty string: unassigned is the genuine
absence of a value, so it should read as null everywhere rather than a sentinel
string that has to be special-cased and backfilled. Every existing chat is null
on migration with no backfill step, and the null default is what new
unassigned chats get. A chat belongs to at most one workflow.

Everything downstream keys on the workflow's uuid, never its name. The name is a
display label the user edits; the id is the stable handle the `workflow_id`
column and every lookup point at. This is the same split chats already use, where
`Rename` retitles by id and nothing keys on the mutable name.

Extend the `Agent` struct in internal/store with a nullable `WorkflowID` field (a
`*string`, so absence is distinct from a set-but-empty id), and the proto
`Session` message with an optional `workflow_id` field. Both flow through the
same plumbing the other fields already use.

## Navigation

The root view gains a fourth row: workflows. Selecting it drills into a
workflow picker, which lists the workflow rows the same way the chats picker
lists chats. It carries the same actions a chat has: r to rename, and so on. A
rename edits the display name only and keys by id, exactly as `Rename` does for
chats today, so nothing referencing the workflow breaks. Selecting a workflow
enters the scoped view.

The scoped view is deliberately a clone of the root: the same new-chat, chats,
and search rows, except every one of them is bound to the selected workflow id.
New chat creates a chat with that `workflow_id` set. Chats lists only chats in
that workflow. Search indexes only that workflow's transcripts.

Concretely this is a scope carried on the model. The model already has a
`screen` enum and the rows it builds. Add a `workflow *store.Workflow` (nil at
root) that the list-building and list-loading paths consult. `loadChatsCmd` and
`reload` call a new workflow-scoped list instead of `ListSessions` when a
workflow is set. `submitSession` passes the scope's id into `NewSession`.
`buildRows` shows a back-to-workflows affordance and the workflow's name in the
header when scoped.

Nothing about the chats picker, the preview, the reconciler, or attach changes.
They all operate on a chat row regardless of which list surfaced it. The scope
is only a filter on which rows the lists show and which id new chats inherit.

## The tricky part: back-navigation after attach

This is the one place the scope has to survive a round trip out of the panel and
back, and it is worth being precise about.

Today the cli loop remembers `focus *store.Agent` across the attach. When you
detach from a chat, the panel reopens on the chats picker with that chat
selected, because `newModel` scans the chats for the focused name and sets the
screen to the chats picker. The loop-back lands you where you were.

With workflows, the requirement is stronger. If you drilled into a workflow,
picked a chat there, and detached, exiting should return you to that workflow's
chats picker, not the parent all-chats list. The parent list would be the wrong
place, because you were working inside a workflow and the workflow is your
context.

The clean way to get this for free is to let the existing focus mechanism carry
the scope. Once `Agent` has a `WorkflowID`, the focused agent already tells the
panel which workflow it belongs to. `newModel` reads `focus.WorkflowID`, sets the
model's workflow scope to it (loading the workflow row by that id), and then
opens on the scoped chats picker with the chat selected. The unassigned case
(`WorkflowID` nil) is exactly today's behavior: no scope, root chats picker.

So the fix is not new state threaded through the cli loop. It is one field on the
resource that already round-trips. The `focus` pointer keeps its meaning; it just
now implies a workflow too.

## The other tricky part: search and restore

Search has two questions to answer: what does it index, and where does opening a
result land you.

Inside a workflow, search should index only that workflow's chats, so
`buildIndexCmd` needs a scoped candidate source alongside the current
`SearchCandidates`, which lists everything via `ListAll`. There is also a case
for a global search reachable from the root that spans every workflow; that can
stay as it is today.

Opening a search result attaches to the chat, and the cli loop restores it if
its pane is gone. Restore already handles the messy parts: a soft-deleted chat
gets undeleted, and a name that was reused while the chat sat dormant gets a
fresh unique name (see `Restore` in internal/agents/manager.go). None of that
logic cares about workflows, and it should not have to. The restored chat keeps
its stored `workflow_id`, because restore round-trips the whole row through
`Upsert`.

The consequence chains through to back-navigation cleanly. When you restore a
chat found in search, it re-enters the workflow it belonged to, because its
`workflow_id` was never touched. On detach, the focus-carries-scope rule above
lands you in that workflow's chats picker. A chat found by a global search that
belongs to workflow B pulls you into workflow B when you exit it, which is the
right thing: you followed the chat to its home.

One decision to make explicit: whether opening a chat from inside workflow A's
scope can ever surface a chat from workflow B. If search is scoped to A, it
cannot, and the model stays simple. Global search is the only place a
cross-workflow jump happens, and there it is intentional.

## What does not change

The reconciler stays chat-only. A workflow does not run a pane and has no
lifecycle state to observe, so it is never reconciled. If the panel wants to show
a per-workflow rollup (how many chats waiting, working, done), that is a pure
aggregation computed on read from the chats already listed, not stored state and
not a reconciler concern. The global status-bar aggregate in `Status` can stay
global, or grow a scoped variant later; it is independent of this change.

Attach, preview, the pane-capture state detection, and the unique-name handling
are all chat-level and untouched. Workflows add a grouping layer above them, not
a change to them.

## Deleting a workflow

Soft-delete mirrors chats: an `is_deleted` flag hides the workflow from the
picker without dropping the row. The open question is what happens to its chats.
Two defensible options. Cascade the hide, so deleting a workflow hides its chats
too, matching the intuition that the bucket and its contents go together.
Or orphan them back to unassigned by setting their `workflow_id` to null, so no
chat is ever lost behind a deleted workflow. Orphaning is safer and reversible and is the
recommended default; cascade can come later if users ask for it.

## Rough migration path

One schema migration adds the `workflows` table and the nullable
`sessions.workflow_id` column. No backfill: existing chats are null, which reads
as unassigned, so the current root view is unchanged the moment the migration
lands.

The proto gains a `Workflow` message and either a `WorkflowService` or a few
workflow RPCs on the existing service, plus the `workflow_id` field on `Session`.
The generated code and the store queries follow: a scoped `ListSessionsByWorkflow`
alongside the existing `ListSessions`, and CRUD for workflows mirroring the
session queries.

The panel gains the workflows picker screen and the scope field on the model,
and the list-building and list-loading paths learn to consult the scope. The cli
loop needs no structural change once `Agent.WorkflowID` exists, because the focus
pointer already carries it.

The smallest shippable first cut is: the table and column, the ability to assign
a chat to a workflow, a workflow picker, and scoped chats. Scoped search and the
delete-cascade decision can follow once the core grouping feels right.

## Decisions

Chats can move between workflows after creation. Reassignment is one write to
`workflow_id` keyed by the chat's id, the same shape as a rename, and it is
reversible, so there is no reason to forbid it. The chats picker gets a move
action alongside rename and delete: it opens the workflow picker, and selecting a
target sets the chat's `workflow_id` to that workflow's id. Setting it to null is
how a chat leaves a workflow. Assign-on-creation is the minimum first cut; the
move action is the natural follow-on.

Search exists in both forms. Inside a workflow it is scoped to that workflow's
chats. From the root it is global and spans every workflow, which is the one
intentional place a result pulls you across workflow boundaries. The scoped and
global index builds differ only in their candidate source.

The root chats picker keeps showing every chat regardless of workflow. This keeps
the flat view intact for anyone who never adopts workflows and makes the feature
purely additive: assigning a chat to a workflow adds a scoped place to find it
without removing it from the top-level list. The workflow view is the filtered
lens; the root stays the whole set.
