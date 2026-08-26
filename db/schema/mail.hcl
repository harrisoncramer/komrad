table "mail" {
  schema  = schema.public
  comment = "One piece of mail from one agent to another, held until the recipient claims it."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "from_agent_id" {
    null = true
    type = uuid
  }
  column "from_name" {
    null    = false
    type    = text
    default = ""
  }
  column "to_agent_id" {
    null = false
    type = uuid
  }
  column "body" {
    null = false
    type = text
  }
  column "reply_to" {
    null = true
    type = uuid
  }
  column "kind" {
    null    = true
    type    = text
    comment = "What sort of notice this is, for the mail weave sends itself and only wants one of waiting at a time, such as the review doorbell. Null is ordinary mail, of which an agent may hold many."
  }
  column "hops" {
    null    = false
    type    = int
    default = 1
  }
  column "claimed_at" {
    null = true
    type = timestamptz
  }
  column "reported_at" {
    null = true
    type = timestamptz
  }
  column "expires_at" {
    null = false
    type = timestamptz
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
  foreign_key "mail_from_agent_id_fkey" {
    columns     = [column.from_agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "mail_to_agent_id_fkey" {
    columns     = [column.to_agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "mail_reply_to_fkey" {
    columns     = [column.reply_to]
    ref_columns = [table.mail.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "mail_pending" {
    columns = [column.to_agent_id, column.id]
    where   = "claimed_at IS NULL"
  }
  index "mail_pending_kind" {
    unique  = true
    columns = [column.to_agent_id, column.kind]
    where   = "claimed_at IS NULL AND kind IS NOT NULL"
    comment = "At most one unclaimed notice of a kind per agent, so a hundred edits to a review do not queue a hundred doorbells the agent has to read through."
  }
  index "mail_from_agent_id" {
    columns = [column.from_agent_id]
  }
  index "mail_reply_to" {
    columns = [column.reply_to]
  }
}
