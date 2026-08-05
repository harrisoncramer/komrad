table "agent_messages" {
  schema  = schema.public
  comment = "One message from one agent to another, held until the recipient claims it."
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
  column "hops" {
    null    = false
    type    = int
    default = 1
  }
  column "claimed_at" {
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
  foreign_key "agent_messages_from_agent_id_fkey" {
    columns     = [column.from_agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "agent_messages_to_agent_id_fkey" {
    columns     = [column.to_agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "agent_messages_reply_to_fkey" {
    columns     = [column.reply_to]
    ref_columns = [table.agent_messages.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "agent_messages_pending" {
    columns = [column.to_agent_id, column.id]
    where   = "claimed_at IS NULL"
  }
  index "agent_messages_from_agent_id" {
    columns = [column.from_agent_id]
  }
  index "agent_messages_reply_to" {
    columns = [column.reply_to]
  }
}
