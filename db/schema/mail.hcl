enum "mail_kind" {
  schema = schema.public
  values = ["plain", "review_activity", "exchange_capped"]
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
    comment = "What the mail is, which is what lets the panel tell a doorbell weave raised itself from something an agent wrote. Ordinary mail is plain. It is also what a doorbell is counted by, since an agent needs only one unread doorbell of a kind however many comments are waiting behind it."
  }
  column "reply_to" {
    null = true
    type = uuid
  }
  column "about_agent_id" {
    null    = true
    type    = uuid
    comment = "The other agent a notice weave raised is about, null on ordinary mail. It is what holds a recipient to one unread notice per subject rather than one per kind, so an agent capped with a second partner is told about that one too. It carries no foreign key, since the notice has to keep saying which exchange was cut off after the agent it names is gone."
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
    comment = "Reads what an agent is holding unread, which is both what a send is refused by the size of and where the doorbell a further release joins is found."
  }
  index "mail_from_agent_id" {
    columns = [column.from_agent_id, column.to_agent_id]
    comment = "Counts the mail one pair of agents has exchanged, which is what a send is refused by the size of."
  }
  index "mail_reply_to" {
    columns = [column.reply_to]
  }
}
