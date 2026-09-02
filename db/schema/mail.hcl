enum "mail_kind" {
  schema = schema.public
  values = ["plain", "review_activity"]
}

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
  column "kind" {
    null    = false
    type    = enum.mail_kind
    default = "plain"
    comment = "What the mail is, which is what lets the panel tell a doorbell weave raised itself from something an agent wrote. Ordinary mail is plain. It is not what decides whether a second copy may stack up: a doorbell is counted by the review comment it points at rather than by its kind."
  }
  column "reply_to" {
    null = true
    type = uuid
  }
  column "review_message_id" {
    null    = true
    type    = uuid
    comment = "The review comment this mail is the doorbell for, null for anything else. It is what one doorbell per comment is counted by, and it is set null rather than cascaded when that comment is deleted, since the agent may already have read the doorbell for a comment the person reviewing then took back."
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
  foreign_key "mail_review_message_id_fkey" {
    columns     = [column.review_message_id]
    ref_columns = [table.review_messages.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  index "mail_pending" {
    columns = [column.to_agent_id, column.id]
    where   = "claimed_at IS NULL"
  }
  index "mail_pending_review_message" {
    unique  = true
    columns = [column.review_message_id]
    where   = "claimed_at IS NULL"
    comment = "At most one unclaimed doorbell per review comment. A doorbell that is still waiting to be read is already saying what a second one would say, so releasing again must not stack another copy behind it. The predicate cannot also ask that the doorbell still be deliverable, since Postgres will not index on NOW(), so InsertMail carries that half itself and rings the row again once it has expired. Mail that is not a doorbell has a null here, and nulls do not collide, so ordinary mail is untouched by it."
  }
  index "mail_review_message_id" {
    columns = [column.review_message_id]
    comment = "Reads the doorbells raised for a comment, claimed ones included, which is what the delivery state of that comment is read off."
  }
  index "mail_from_agent_id" {
    columns = [column.from_agent_id]
  }
  index "mail_reply_to" {
    columns = [column.reply_to]
  }
}
