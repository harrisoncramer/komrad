enum "review_author_kind" {
  schema = schema.public
  values = ["human", "agent"]
}

table "review_threads" {
  schema  = schema.public
  comment = "One conversation pinned to a place in an agent's diff."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "agent_id" {
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
    comment = "The diff lines the thread was written against. They say what the thread was about once the agent has rewritten that code, and are what the viewer compares the file against to decide whether the anchor still holds."
  }
  column "last_agent_activity_at" {
    null    = true
    type    = timestamptz
    comment = "When the agent last wrote or rewrote a message here, null until it has written one. Kept as a column rather than read off the messages, so that an agent rewriting its own reply is activity the person reviewing has not seen, which a comparison against created_at cannot see. There is no mirror of it for the person's side, since what the agent has been told is recorded per message by review_messages.sent_at and the mail raised against it."
  }
  column "human_read_at" {
    null    = true
    type    = timestamptz
    comment = "When the person reviewing last read this thread, stamped from last_agent_activity_at rather than from the clock, so a reply written mid-read is not marked read without being shown. It is what says whether the agent has replied since they looked."
  }
  column "resolved_at" {
    null    = true
    type    = timestamptz
    comment = "When the person reviewing settled this thread, null while it is still owed an answer. It is what decides whether a thread is still asking for something, which is a different question from whether the code it was written against has moved since."
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
  index "review_threads_agent_id" {
    columns = [column.agent_id, column.file_path, column.new_start, column.old_start]
  }
}

table "review_messages" {
  schema  = schema.public
  comment = "One message in a review thread, written either by the person reviewing or by the agent replying."
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
  column "sent_at" {
    null    = true
    type    = timestamptz
    comment = "When this message was released to the agent, null while it is still only written down. It is the durable record that a comment was let go of, kept here rather than read off the mail raised for it, since an agent may delete that mail and mail expires."
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
  foreign_key "review_messages_author_agent_id_fkey" {
    columns     = [column.author_agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "review_messages_thread_id" {
    columns = [column.thread_id, column.id]
  }
}

table "review_viewed_files" {
  schema  = schema.public
  comment = "One file of an agent's diff the person reviewing has read, and the shape that file had when they read it."
  column "agent_id" {
    null = false
    type = uuid
  }
  column "file_path" {
    null    = false
    type    = text
    comment = "The file that was read, named the way the diff names it: relative to the repository root, where the change left it if it moved it."
  }
  column "fingerprint" {
    null    = false
    type    = text
    comment = "A hash of the file's own slice of the patch as it stood when it was read. The panel hashes the file again on every load and shows it as unread once the two disagree, which is what makes an agent writing to the file undo the mark. It is the diff that is hashed rather than the file, since the hunks change when the base moves under a file nothing has touched."
  }
  column "viewed_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  primary_key {
    columns = [column.agent_id, column.file_path]
  }
  foreign_key "review_viewed_files_agent_id_fkey" {
    columns     = [column.agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
}
