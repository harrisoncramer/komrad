enum "review_author_kind" {
  schema = schema.public
  values = ["human", "agent", "forge"]
}

enum "review_verdict" {
  schema = schema.public
  values = ["approved", "changes_requested", "commented"]
}

enum "forge_reply_state" {
  schema = schema.public
  values = ["draft", "approved", "sending", "sent", "failed"]
}

enum "forge_reply_approver_kind" {
  schema = schema.public
  values = ["human"]
}

table "review_snapshots" {
  schema  = schema.public
  comment = "One frozen copy of what an agent has written. A review is always given against a snapshot rather than against the working files."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "task_id" {
    null    = false
    type    = uuid
    comment = "The task whose checkout the snapshot froze."
  }
  column "ordinal" {
    null    = false
    type    = int
    comment = "Which snapshot this is for its task, counting from one. The panel names the snapshot by this number."
  }
  column "base_sha" {
    null    = false
    type    = text
    comment = "The commit the agent's branch started from. It is resolved once, when the snapshot is taken, so later movement on the base branch cannot change what the snapshot says."
  }
  column "sha" {
    null    = false
    type    = text
    comment = "A commit covering the agent's whole checkout, tracked and untracked files alike. A ref under refs/komrad/snapshots keeps it alive so git will not collect it. Writing it leaves the agent's own branch, index and files alone."
  }
  column "landed_at" {
    null    = true
    type    = timestamptz
    comment = "When the base branch was first seen holding this snapshot whole, or a later snapshot whole, null until then. A landed snapshot's settled comments are history rather than work still under review."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "review_snapshots_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "review_snapshots_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "review_snapshots_task_id_fkey" {
    columns     = [column.engine_id, column.task_id]
    ref_columns = [table.tasks.column.engine_id, table.tasks.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "review_snapshots_task_ordinal" {
    columns = [column.task_id, column.ordinal]
    unique  = true
  }
}

table "reviews" {
  schema  = schema.public
  comment = "One reviewer's batch of comments on one snapshot. Nobody else sees it while it is being written, and it is released all at once."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "task_id" {
    null    = false
    type    = uuid
    comment = "The task being reviewed. The snapshot already says this, and it is repeated here so that one open review per reviewer can be a plain unique index with no join."
  }
  column "snapshot_id" {
    null = false
    type = uuid
  }
  column "author_kind" {
    null    = false
    type    = enum.review_author_kind
    comment = "Whether a person or an agent is giving the review."
  }
  column "author_agent_id" {
    null    = true
    type    = uuid
    comment = "The agent giving the review, null when a person is giving it. Do not read null as meaning a person, because author_kind is what says that. Deleting the reviewing agent clears this column rather than deleting the review. Reviewer agents are deleted as a matter of course, and the agent they reviewed is still working from what they said."
  }
  column "verdict" {
    null    = true
    type    = enum.review_verdict
    comment = "What the reviewer concluded, null while the review is still open. It is how the agent learns whether the work is done, and only an approval lets it open a pull request."
  }
  column "released_at" {
    null    = true
    type    = timestamptz
    comment = "When the review was released, null while it is still being written. None of it reaches the agent until this is set."
  }
  column "path" {
    null    = false
    type    = text
    default = ""
    comment = "The absolute path of the file the released review was written to, empty while the review is still open. The file lives in komrad's own state directory rather than in the agent's checkout."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "updated_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "body" {
    null    = false
    type    = text
    default = ""
    comment = "What the reviewer said about the change as a whole rather than about any one line, empty when they said nothing. It goes at the top of the released review file, above the line comments, because it is what the agent should read first."
  }
  column "external_login" {
    null    = true
    type    = text
    comment = "The forge login of whoever wrote a review on the forge, empty when the forge no longer knew them. Null for a review written in komrad."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "reviews_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "reviews_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "reviews_task_id_fkey" {
    columns     = [column.engine_id, column.task_id]
    ref_columns = [table.tasks.column.engine_id, table.tasks.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "reviews_snapshot_id_fkey" {
    columns     = [column.engine_id, column.snapshot_id]
    ref_columns = [table.review_snapshots.column.engine_id, table.review_snapshots.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "reviews_author_agent_id_fkey" {
    columns     = [column.engine_id, column.author_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "reviews_open_agent_task" {
    columns = [column.task_id, column.author_agent_id]
    unique  = true
    where   = "released_at IS NULL AND author_kind = 'agent'"
    comment = "Holds each reviewing agent to one unreleased review of a task at a time, relying on Postgres counting two null authors as different values."
  }
  index "reviews_task_id" {
    columns = [column.task_id, column.id]
  }
  index "reviews_snapshot_id" {
    columns = [column.snapshot_id, column.id]
  }
}

table "review_threads" {
  schema  = schema.public
  comment = "One conversation pinned to a file and a line range in the snapshot its review was given against."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "task_id" {
    null    = false
    type    = uuid
    comment = "The task being reviewed. The review already says this and neither value ever changes. It is repeated here because almost every read of threads is by task."
  }
  column "file_path" {
    null    = false
    type    = text
    comment = "The file the thread is pinned to, named the way the diff names it. That is a path relative to the repository root, and where the change left the file if it moved it."
  }
  column "old_start" {
    null    = false
    type    = int
    default = 0
    comment = "First line of the range on the old side of the diff. Zero means the thread is pinned to lines that exist only on the new side."
  }
  column "old_end" {
    null    = false
    type    = int
    default = 0
    comment = "Last line of the range on the old side. It equals old_start for a thread on a single line."
  }
  column "new_start" {
    null    = false
    type    = int
    default = 0
    comment = "First line of the range on the new side of the diff. Zero means the thread is pinned to lines the change deleted."
  }
  column "new_end" {
    null    = false
    type    = int
    default = 0
    comment = "Last line of the range on the new side. It equals new_start for a thread on a single line."
  }
  column "quoted_lines" {
    null    = false
    type    = sql("text[]")
    default = sql("'{}'::text[]")
    comment = "The diff lines the thread was written against. They still say what the thread was about after the agent has rewritten that code. A later snapshot is searched for these lines to decide whether the thread has gone outdated."
  }
  column "human_read_at" {
    null    = true
    type    = timestamptz
    comment = "When the person reviewing last read this thread. It is copied from last_agent_activity_at rather than taken from the clock, so a reply written while they were reading is not marked read without having been shown."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "updated_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "last_agent_activity_at" {
    null    = true
    type    = timestamptz
    comment = "When the agent last wrote or rewrote a message here, null until it has written one. It is a column rather than something read off the messages. An agent that rewrites its own reply is activity the person reviewing has not seen, and comparing against the message's created_at would miss that."
  }
  column "resolved_at" {
    null    = true
    type    = timestamptz
    comment = "When this thread was settled, null while it is still owed an answer."
  }
  column "review_id" {
    null    = true
    type    = uuid
    comment = "The review the thread was opened in, null on a thread a person opened, which belongs to no review."
  }
  column "author_kind" {
    null    = false
    type    = enum.review_author_kind
    comment = "Who opened the thread."
  }
  column "author_agent_id" {
    null    = true
    type    = uuid
    comment = "The agent that opened the thread, null when a person or a forge user did, or once the agent was deleted."
  }
  column "snapshot_id" {
    null    = false
    type    = uuid
    comment = "The snapshot this thread's line numbers were measured against. It is not read through the review, because a review stays open across several snapshots and would name the wrong one."
  }
  column "base_sha" {
    null    = false
    type    = text
    default = ""
    comment = "The commit the old side of this thread's line numbers was read against. For a thread written on the whole change that is the snapshot's own base. For a thread written while the diff was narrowed to one snapshot it is the earlier snapshot's commit."
  }
  column "resolved_by_kind" {
    null    = true
    type    = enum.review_author_kind
    comment = "Whether a person or an agent settled this thread, null while nobody has. Two parties can settle one: the agent that wrote the comment, saying the answer was good enough, and the person, who may settle anything including a thread whose author is gone."
  }
  column "resolved_by_agent_id" {
    null    = true
    type    = uuid
    comment = "The agent that settled this thread, null when a person did or when nobody has. Do not read null as meaning a person, because resolved_by_kind is what says that. Deleting that agent clears this column rather than the settling."
  }
  column "forge_thread_id" {
    null    = true
    type    = uuid
    comment = "The forge thread this thread mirrors, null for a thread written in komrad or once the forge has deleted it."
  }
  column "forge_resolved" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether the forge said the thread was resolved as of the last sync, so only a change there resolves or reopens it here."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "review_threads_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "review_threads_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "review_threads_task_id_fkey" {
    columns     = [column.engine_id, column.task_id]
    ref_columns = [table.tasks.column.engine_id, table.tasks.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_threads_review_id_fkey" {
    columns     = [column.engine_id, column.review_id]
    ref_columns = [table.reviews.column.engine_id, table.reviews.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_threads_snapshot_id_fkey" {
    columns     = [column.engine_id, column.snapshot_id]
    ref_columns = [table.review_snapshots.column.engine_id, table.review_snapshots.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_threads_author_agent_id_fkey" {
    columns     = [column.engine_id, column.author_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "review_threads_resolved_by_agent_id_fkey" {
    columns     = [column.engine_id, column.resolved_by_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "review_threads_forge_thread_id_fkey" {
    columns     = [column.engine_id, column.forge_thread_id]
    ref_columns = [table.forge_review_threads.column.engine_id, table.forge_review_threads.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "review_threads_review_id" {
    columns = [column.review_id, column.id]
  }
  index "review_threads_forge_thread_id" {
    columns = [column.forge_thread_id]
  }
  index "review_threads_task_id" {
    columns = [column.task_id, column.file_path, column.new_start, column.old_start]
  }
  index "review_threads_task_forge_thread" {
    columns = [column.task_id, column.forge_thread_id]
    unique  = true
    where   = "forge_thread_id IS NOT NULL"
    comment = "Holds each task to one mirror of a forge thread."
  }
}

table "review_messages" {
  schema  = schema.public
  comment = "One message in a review thread, written either by the reviewer or by the agent answering them."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "thread_id" {
    null = false
    type = uuid
  }
  column "author_kind" {
    null    = false
    type    = enum.review_author_kind
    comment = "Whether a person or an agent wrote the message."
  }
  column "author_agent_id" {
    null    = true
    type    = uuid
    comment = "The agent that wrote this message, null when a person wrote it or when that agent has since been deleted."
  }
  column "body" {
    null    = false
    type    = text
    comment = "What was written, as markdown."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "updated_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "review_id" {
    null    = true
    type    = uuid
    comment = "The review this message was released with, null for an answer from the agent. An answer belongs to no batch and reaches the reviewer as soon as it is written."
  }
  column "suggestion" {
    null    = false
    type    = sql("text[]")
    default = sql("'{}'::text[]")
    comment = "Replacement lines the reviewer is proposing for the range the thread is pinned to, empty on an ordinary comment. The agent applies them as written rather than treating them as a description of what to do."
  }
  column "snapshot_id" {
    null    = true
    type    = uuid
    comment = "The snapshot an agent's reply said answers the thread, taken of its worktree as it stood when it replied. Set only on a reply that resolves the thread; null on everything else."
  }
  column "external_login" {
    null    = true
    type    = text
    comment = "The forge login of whoever wrote this on the forge, for a comment that came from there, empty when the forge no longer knew them. Null for anything written in komrad."
  }
  column "forge_comment_id" {
    null    = true
    type    = uuid
    comment = "The forge comment this mirrors, null for anything written in komrad. It names no row once the forge deletes the comment, which is how the copy is known to be gone there."
  }
  column "forge_edited_at" {
    null    = true
    type    = timestamptz
    comment = "When the forge comment had last been edited as of the last sync, null when it never had been."
  }
  column "held" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether the forge author could not write to the repository when the text was written or last edited, which keeps it from the agent. A held first message holds its whole thread until the person accepts it."
  }
  column "accepted_from" {
    null    = true
    type    = uuid
    comment = "The held forge message the person accepted this copy of, which releases that message's thread to the agent. Null on everything else."
  }
  column "mail_id" {
    null    = true
    type    = uuid
    comment = "The mail that carried a person's comment to the writer, null on an agent's or a forge user's comment. Deleting that mail clears this column and nothing else."
  }
  column "read_at" {
    null    = true
    type    = timestamptz
    comment = "When the writer read the mail carrying a person's comment, null until then and on every other comment."
  }
  column "read_snapshot_id" {
    null    = true
    type    = uuid
    comment = "The snapshot taken when the writer read a person's comment, which an addressed reply is compared against."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "review_messages_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "review_messages_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "review_messages_thread_id_fkey" {
    columns     = [column.engine_id, column.thread_id]
    ref_columns = [table.review_threads.column.engine_id, table.review_threads.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_messages_review_id_fkey" {
    columns     = [column.engine_id, column.review_id]
    ref_columns = [table.reviews.column.engine_id, table.reviews.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "review_messages_snapshot_id_fkey" {
    columns     = [column.engine_id, column.snapshot_id]
    ref_columns = [table.review_snapshots.column.engine_id, table.review_snapshots.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_messages_accepted_from_fkey" {
    columns     = [column.engine_id, column.accepted_from]
    ref_columns = [table.review_messages.column.engine_id, table.review_messages.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "review_messages_author_agent_id_fkey" {
    columns     = [column.engine_id, column.author_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "review_messages_mail_id_fkey" {
    columns     = [column.engine_id, column.mail_id]
    ref_columns = [table.mail.column.engine_id, table.mail.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "review_messages_read_snapshot_id_fkey" {
    columns     = [column.engine_id, column.read_snapshot_id]
    ref_columns = [table.review_snapshots.column.engine_id, table.review_snapshots.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "review_messages_thread_id" {
    columns = [column.thread_id, column.id]
  }
  index "review_messages_accepted_from" {
    columns = [column.accepted_from]
  }
  index "review_messages_review_id" {
    columns = [column.review_id]
  }
  index "review_messages_mail_id" {
    columns = [column.mail_id]
  }
  index "review_messages_thread_forge_comment" {
    columns = [column.thread_id, column.forge_comment_id]
    unique  = true
    where   = "forge_comment_id IS NOT NULL"
    comment = "Holds each mirrored thread to one copy of a forge comment."
  }
}

table "review_forge_replies" {
  schema  = schema.public
  comment = "A reply komrad posts to the forge on a mirrored thread, from the agent's proposal or the person's own words through to the forge comment it became."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "thread_id" {
    null    = false
    type    = uuid
    comment = "The mirrored review thread the reply answers on the forge."
  }
  column "state" {
    null    = false
    type    = enum.forge_reply_state
    comment = "Where the reply is on its way to the forge. A draft waits for the person, an approved reply waits to be posted, and a sending one may already be on the forge."
  }
  column "author_kind" {
    null    = false
    type    = enum.review_author_kind
    comment = "Whether the person or the agent wrote the reply."
  }
  column "author_agent_id" {
    null    = true
    type    = uuid
    comment = "The agent that proposed the reply, null when the person wrote it or when that agent has since been deleted."
  }
  column "proposed_body" {
    null    = true
    type    = text
    comment = "The agent's text as it proposed it, kept so the agent can learn whether its draft went out as written. Null on a reply the person wrote."
  }
  column "body" {
    null    = false
    type    = text
    comment = "The text to post, as markdown, which the person may have edited from the proposal. Once sent it is what the forge received."
  }
  column "approver_kind" {
    null    = true
    type    = enum.forge_reply_approver_kind
    comment = "Who approved the reply for posting, null while it is a draft."
  }
  column "approved_at" {
    null    = true
    type    = timestamptz
    comment = "When the reply was approved for posting, null while it is a draft."
  }
  column "sending_started_at" {
    null    = true
    type    = timestamptz
    comment = "When the latest attempt to post the reply began, null until one has."
  }
  column "sent_at" {
    null    = true
    type    = timestamptz
    comment = "When the forge accepted the reply, null until it has."
  }
  column "forge_comment_provider_id" {
    null    = true
    type    = text
    comment = "The forge's own id for the comment the reply became, which the sync matches so it does not mirror the reply back as a new comment. Null until the forge accepts it."
  }
  column "error" {
    null    = true
    type    = text
    comment = "What the forge said when the latest attempt to post failed, null otherwise."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "updated_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "review_forge_replies_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "review_forge_replies_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "review_forge_replies_thread_id_fkey" {
    columns     = [column.engine_id, column.thread_id]
    ref_columns = [table.review_threads.column.engine_id, table.review_threads.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_forge_replies_author_agent_id_fkey" {
    columns     = [column.engine_id, column.author_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "review_forge_replies_thread_id" {
    columns = [column.thread_id, column.id]
  }
  index "review_forge_replies_open_proposal" {
    columns = [column.thread_id]
    unique  = true
    where   = "author_kind = 'agent' AND state <> 'sent'"
    comment = "Holds each thread to one agent proposal that has not yet reached the forge."
  }
  index "review_forge_replies_thread_forge_comment" {
    columns = [column.thread_id, column.forge_comment_provider_id]
    unique  = true
    where   = "forge_comment_provider_id IS NOT NULL"
    comment = "Holds each forge comment to one reply that became it."
  }
  index "review_forge_replies_sending" {
    columns = [column.id]
    where   = "state = 'sending'"
    comment = "Finds the replies an interrupted send may have left on the forge."
  }
  check "review_forge_replies_author_not_forge" {
    expr = "author_kind IN ('human', 'agent')"
  }
  check "review_forge_replies_proposal_from_agent" {
    expr = "(author_kind = 'agent') = (proposed_body IS NOT NULL)"
  }
  check "review_forge_replies_human_skips_draft" {
    expr = "author_kind = 'agent' OR state <> 'draft'"
  }
  check "review_forge_replies_approval_recorded" {
    expr = "(state = 'draft') = (approver_kind IS NULL) AND (approver_kind IS NULL) = (approved_at IS NULL)"
  }
  check "review_forge_replies_sending_started" {
    expr = "state IN ('draft', 'approved') OR sending_started_at IS NOT NULL"
  }
  check "review_forge_replies_sent_recorded" {
    expr = "(state = 'sent') = (sent_at IS NOT NULL) AND (state = 'sent') = (forge_comment_provider_id IS NOT NULL)"
  }
  check "review_forge_replies_failure_recorded" {
    expr = "(state = 'failed') = (error IS NOT NULL)"
  }
  check "review_forge_replies_body_written" {
    expr = "body <> ''"
  }
}

table "review_viewed_files" {
  schema  = schema.public
  comment = "One file of an agent's diff the person reviewing has read, and the shape that file had when they read it."
  column "file_path" {
    null    = false
    type    = text
    comment = "The file that was read, named the way the diff names it: relative to the repository root, where the change left it if it moved it."
  }
  column "viewed_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "task_id" {
    null    = false
    type    = uuid
    comment = "The task whose diff the file is in."
  }
  column "fingerprint" {
    null    = false
    type    = text
    comment = "A hash of the file's own slice of the patch as it stood when it was read. The panel hashes the file again on every load and shows it as unread once the two disagree, which is what makes an agent writing to the file undo the mark. It is the diff that is hashed rather than the file, since the hunks change when the base moves under a file nothing has touched."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.task_id, column.file_path]
  }
  foreign_key "review_viewed_files_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "review_viewed_files_task_id_fkey" {
    columns     = [column.engine_id, column.task_id]
    ref_columns = [table.tasks.column.engine_id, table.tasks.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
}
