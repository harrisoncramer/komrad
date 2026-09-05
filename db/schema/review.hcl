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
  comment = "One immutable snapshot of what an agent has written, which is what a review is given against."
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
    comment = "Which round this is for its agent, counting from one, which is what the panel names it by."
  }
  column "base_sha" {
    null    = false
    type    = text
    comment = "The commit the agent's branch left its base at, resolved when the round was taken so that a base moving afterwards cannot change what this round says."
  }
  column "snapshot_sha" {
    null    = false
    type    = text
    comment = "A commit written out of band over the agent's whole worktree, tracked and untracked alike, held by a ref under refs/weave/rounds so it survives collection. The agent's own index, worktree and branch are untouched by it."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  primary_key {
    columns = [column.id]
  }
  foreign_key "review_rounds_agent_id_fkey" {
    columns     = [column.agent_id]
    ref_columns = [table.agents.column.id]
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
  comment = "One author's batch of comments against one round, private while it is being written and released as a whole."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "agent_id" {
    null    = false
    type    = uuid
    comment = "The agent being reviewed, carried here as well as on the round so that one open review per author can be enforced without a join."
  }
  column "round_id" {
    null = false
    type = uuid
  }
  column "author_kind" {
    null    = false
    type    = enum.review_author_kind
    comment = "The type of the author giving the review."
  }
  column "author_agent_id" {
    null    = true
    type    = uuid
    comment = "The agent giving the review, null when the person reviewing is. Null does not itself mean the person, author_kind says that. Deleting that agent takes its reviews with it rather than nulling this, since a null here would put an open batch outside the index that holds an author to one at a time and beyond any call that could find it again."
  }
  column "verdict" {
    null    = true
    type    = enum.review_verdict
    comment = "What the author concluded, null while the review is still open. It is what tells the agent whether it is done, and what gates opening a pull request."
  }
  column "released_at" {
    null    = true
    type    = timestamptz
    comment = "When the review was let go of, null while it is still being written. Nothing in it reaches the agent before this is stamped."
  }
  column "path" {
    null    = false
    type    = text
    default = ""
    comment = "The absolute path the released review was written to, under weave's own state directory rather than in the agent's worktree, empty while the review is still open."
  }
  column "doorbell_mail_id" {
    null    = true
    type    = uuid
    comment = "The mail that told the agent the review was waiting. It is set null rather than cascaded when that mail is deleted, which is what makes a review the agent threw the notice away for read as undelivered."
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
  primary_key {
    columns = [column.id]
  }
  foreign_key "reviews_agent_id_fkey" {
    columns     = [column.agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "reviews_round_id_fkey" {
    columns     = [column.round_id]
    ref_columns = [table.review_rounds.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "reviews_author_agent_id_fkey" {
    columns     = [column.author_agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "reviews_doorbell_mail_id_fkey" {
    columns     = [column.doorbell_mail_id]
    ref_columns = [table.mail.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "reviews_open_human" {
    columns = [column.agent_id]
    unique  = true
    where   = "released_at IS NULL AND author_kind = 'human'"
    comment = "One open review at a time for the person, the way a forge holds one pending review per reviewer."
  }
  index "reviews_open_agent" {
    columns = [column.agent_id, column.author_agent_id]
    unique  = true
    where   = "released_at IS NULL AND author_kind = 'agent'"
    comment = "One open review at a time for each agent reviewing, so a reviewer cannot draft two at once."
  }
  index "reviews_round_id" {
    columns = [column.round_id, column.id]
  }
  index "reviews_doorbell_mail_id" {
    columns = [column.doorbell_mail_id]
    comment = "Finds the review a piece of mail is the doorbell for, which is what the foreign key walks when that mail is deleted."
  }
}

table "review_threads" {
  schema  = schema.public
  comment = "One conversation pinned to a place in the round its review was given against."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "agent_id" {
    null    = false
    type    = uuid
    comment = "The agent being reviewed, carried here as well as on the review since neither ever moves and almost every read is by agent."
  }
  column "review_id" {
    null = false
    type = uuid
  }
  column "file_path" {
    null    = false
    type    = text
    comment = "The file the thread is pinned to, named the way the diff names it: relative to the repository root, where the change left it if it moved it."
  }
  column "old_start" {
    null    = false
    type    = int
    default = 0
    comment = "First line of the range on the old side of the diff, or zero for a thread pinned to lines that exist only on the new side."
  }
  column "old_end" {
    null    = false
    type    = int
    default = 0
    comment = "Last line of the range on the old side, equal to old_start for a thread on a single line."
  }
  column "new_start" {
    null    = false
    type    = int
    default = 0
    comment = "First line of the range on the new side of the diff, or zero for a thread pinned to lines the change deleted."
  }
  column "new_end" {
    null    = false
    type    = int
    default = 0
    comment = "Last line of the range on the new side, equal to new_start for a thread on a single line."
  }
  column "quoted_lines" {
    null    = false
    type    = sql("text[]")
    default = sql("'{}'::text[]")
    comment = "The diff lines the thread was written against. They say what the thread was about once the agent has rewritten that code, and are what a later round is searched for to decide whether the thread has gone outdated."
  }
  column "last_agent_activity_at" {
    null    = true
    type    = timestamptz
    comment = "When the agent last wrote or rewrote a message here, null until it has written one. Kept as a column rather than read off the messages, so that an agent rewriting its own reply is activity the person reviewing has not seen, which a comparison against created_at cannot see."
  }
  column "human_read_at" {
    null    = true
    type    = timestamptz
    comment = "When the person reviewing last read this thread, stamped from last_agent_activity_at rather than from the clock, so a reply written mid-read is not marked read without being shown."
  }
  column "resolved_at" {
    null    = true
    type    = timestamptz
    comment = "When the person reviewing settled this thread, null while it is still owed an answer. Settling stays theirs even for a thread an agent opened, since deciding the work is done is not the reviewer's call."
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
  primary_key {
    columns = [column.id]
  }
  foreign_key "review_threads_agent_id_fkey" {
    columns     = [column.agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_threads_review_id_fkey" {
    columns     = [column.review_id]
    ref_columns = [table.reviews.column.id]
    on_delete   = CASCADE
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
  comment = "One message in a review thread, written either by the author giving the review or by the agent answering it."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "thread_id" {
    null = false
    type = uuid
  }
  column "review_id" {
    null    = true
    type    = uuid
    comment = "The review this message went out with, null for an answer, which belongs to no batch and reaches the reviewer as soon as it is written."
  }
  column "author_kind" {
    null    = false
    type    = enum.review_author_kind
    comment = "The type of the author who wrote the message."
  }
  column "author_agent_id" {
    null    = true
    type    = uuid
    comment = "The agent that wrote this message, if relevant, kept as a link to who it was."
  }
  column "body" {
    null    = false
    type    = text
    comment = "What was written, as markdown."
  }
  column "suggestion" {
    null    = false
    type    = sql("text[]")
    default = sql("'{}'::text[]")
    comment = "Replacement lines the author is proposing for the range the thread is pinned to, empty for an ordinary comment. The agent applies them rather than reasoning about them."
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
  primary_key {
    columns = [column.id]
  }
  foreign_key "review_messages_thread_id_fkey" {
    columns     = [column.thread_id]
    ref_columns = [table.review_threads.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "review_messages_review_id_fkey" {
    columns     = [column.review_id]
    ref_columns = [table.reviews.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "review_messages_author_agent_id_fkey" {
    columns     = [column.author_agent_id]
    ref_columns = [table.agents.column.id]
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
  comment = "One file of a round the person reviewing has read. A later round is a different row, which is what makes an agent rewriting the file undo the mark."
  column "round_id" {
    null = false
    type = uuid
  }
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
  primary_key {
    columns = [column.round_id, column.file_path]
  }
  foreign_key "review_viewed_files_round_id_fkey" {
    columns     = [column.round_id]
    ref_columns = [table.review_rounds.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
}
