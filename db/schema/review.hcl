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
  column "resolved_at" {
    null    = true
    type    = timestamptz
    comment = "When the thread was settled. A message written afterwards reopens it, since late activity in a closed thread would otherwise be hidden."
  }
  column "last_human_activity_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
    comment = "When the person reviewing last wrote or rewrote a message here. Compared against agent_read_at to decide what the agent has not been told, so that rewriting a message counts as activity rather than hiding behind its original created_at."
  }
  column "agent_read_at" {
    null    = true
    type    = timestamptz
    comment = "When the agent last read this thread, stamped to the newest activity it was actually shown rather than to the clock, so a message written mid-read is not marked read without being returned."
  }
  column "human_read_at" {
    null    = true
    type    = timestamptz
    comment = "When the person reviewing last read this thread, which is what says whether the agent has replied since they looked."
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
