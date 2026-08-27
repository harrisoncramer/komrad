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
    comment = "What the mail is, which is what lets a notice that only says there is something to look at be sent again without stacking up. Ordinary mail is plain and never deduplicated, since two agents saying the same thing twice are two things to read."
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
    where   = "claimed_at IS NULL AND kind <> 'plain'::mail_kind"
    comment = "At most one unclaimed notice of a given kind per agent. A doorbell that is still waiting to be read is already saying what a second one would say, so pressing the key again must not stack another copy behind it. The predicate cannot also ask that the notice still be deliverable, since Postgres will not index on NOW(), so InsertMail carries that half itself and rings the row again once it has expired."
  }
  index "mail_from_agent_id" {
    columns = [column.from_agent_id]
  }
  index "mail_reply_to" {
    columns = [column.reply_to]
  }
}
