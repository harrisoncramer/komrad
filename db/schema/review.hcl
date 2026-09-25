enum "review_author_kind" {
  schema = schema.public
  values = ["human", "agent"]
}

enum "review_verdict" {
  schema = schema.public
  values = ["approved", "changes_requested", "commented"]
}

table "review_rounds" {
  schema  = schema.public
  comment = "One frozen snapshot of what an agent has written. A review is always given against a round rather than against the working files."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "agent_id" {
    null = false
    type = uuid
  }
  column "ordinal" {
    null    = false
    type    = int
    comment = "Which round this is for its agent, counting from one. The panel names the round by this number."
  }
  column "base_sha" {
    null    = false
    type    = text
    comment = "The commit the agent's branch started from. It is resolved once, when the round is taken, so later movement on the base branch cannot change what the round says."
  }
  column "snapshot_sha" {
    null    = false
    type    = text
    comment = "A commit covering the agent's whole checkout, tracked and untracked files alike. A ref under refs/komrad/rounds keeps it alive so git will not collect it. Writing it leaves the agent's own branch, index and files alone."
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
  unique "review_rounds_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "review_rounds_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "review_rounds_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "review_rounds_agent_ordinal" {
    columns = [column.agent_id, column.ordinal]
    unique  = true
  }
}

table "reviews" {
  schema  = schema.public
  comment = "One reviewer's batch of comments on one round. Nobody else sees it while it is being written, and it is released all at once."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "agent_id" {
    null    = false
    type    = uuid
    comment = "The agent being reviewed. The round already says this, and it is repeated here so that one open review per reviewer can be a plain unique index with no join."
  }
  column "round_id" {
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
  column "doorbell_mail_id" {
    null    = true
    type    = uuid
    comment = "The mail that told the agent a review was waiting. Deleting that mail clears this column rather than deleting the review, so a review whose notice was thrown away reads as undelivered."
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
  foreign_key "reviews_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "reviews_round_id_fkey" {
    columns     = [column.engine_id, column.round_id]
    ref_columns = [table.review_rounds.column.engine_id, table.review_rounds.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "reviews_author_agent_id_fkey" {
    columns     = [column.engine_id, column.author_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "reviews_doorbell_mail_id_fkey" {
    columns     = [column.engine_id, column.doorbell_mail_id]
    ref_columns = [table.mail.column.engine_id, table.mail.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "reviews_open_human" {
    columns = [column.agent_id]
    unique  = true
    where   = "released_at IS NULL AND author_kind = 'human'"
    comment = "Holds the person to one unreleased review at a time, the way GitHub holds one pending review per reviewer."
  }
  index "reviews_open_agent" {
    columns = [column.agent_id, column.author_agent_id]
    unique  = true
    where   = "released_at IS NULL AND author_kind = 'agent'"
    comment = "Holds each reviewing agent to one unreleased review at a time. Postgres counts two nulls in this column as different values, and the index relies on that. If nulls collided, deleting a second reviewer agent would fail, because the foreign key clears this column and would land on the null the first delete left behind."
  }
  index "reviews_round_id" {
    columns = [column.round_id, column.id]
  }
  index "reviews_doorbell_mail_id" {
    columns = [column.doorbell_mail_id]
    comment = "Finds the review a piece of mail was the notice for. Deleting that mail walks this index."
  }
}

table "review_threads" {
  schema  = schema.public
  comment = "One conversation pinned to a file and a line range in the round its review was given against."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "agent_id" {
    null    = false
    type    = uuid
    comment = "The agent being reviewed. The review already says this and neither value ever changes. It is repeated here because almost every read of threads is by agent."
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
    comment = "The diff lines the thread was written against. They still say what the thread was about after the agent has rewritten that code. A later round is searched for these lines to decide whether the thread has gone outdated."
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
    null = false
    type = uuid
  }
  column "round_id" {
    null    = false
    type    = uuid
    comment = "The round this thread's line numbers were measured against. It is not read through the review, because a review stays open across several rounds and would name the wrong one."
  }
  column "base_sha" {
    null    = false
    type    = text
    default = ""
    comment = "The commit the old side of this thread's line numbers was read against. For a thread written on the whole change that is the round's own base. For a thread written while the diff was narrowed to one round it is the earlier round's snapshot."
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
  foreign_key "review_threads_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_threads_review_id_fkey" {
    columns     = [column.engine_id, column.review_id]
    ref_columns = [table.reviews.column.engine_id, table.reviews.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_threads_round_id_fkey" {
    columns     = [column.engine_id, column.round_id]
    ref_columns = [table.review_rounds.column.engine_id, table.review_rounds.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_threads_resolved_by_agent_id_fkey" {
    columns     = [column.engine_id, column.resolved_by_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "review_threads_agent_id" {
    columns = [column.agent_id, column.file_path, column.new_start, column.old_start]
  }
  index "review_threads_review_id" {
    columns = [column.review_id, column.id]
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
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
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
  foreign_key "review_messages_author_agent_id_fkey" {
    columns     = [column.engine_id, column.author_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "review_messages_thread_id" {
    columns = [column.thread_id, column.id]
  }
  index "review_messages_review_id" {
    columns = [column.review_id]
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
  column "agent_id" {
    null = false
    type = uuid
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
    columns = [column.agent_id, column.file_path]
  }
  foreign_key "review_viewed_files_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "review_viewed_files_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
}
