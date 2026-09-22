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
  column "reply_to" {
    null = true
    type = uuid
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
  column "kind" {
    null    = false
    type    = enum.mail_kind
    default = "plain"
    comment = "Who wrote the mail and why. Plain is mail an agent sent. The other values are notices weave raised itself, which the panel draws differently. Weave also counts unread notices by this value. One unread notice of a kind is enough however much is waiting behind it."
  }
  column "about_agent_id" {
    null    = true
    type    = uuid
    comment = "The other agent a weave notice is about, null on ordinary mail. Counting unread notices by this column gives the recipient one notice per subject rather than one per kind, so an agent that hits the exchange cap with a second partner is told about that partner too. There is no foreign key here on purpose. The notice has to keep naming the agent whose exchange was cut off after that agent is gone."
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
    comment = "Reads the mail an agent has not claimed yet. A send is refused when that is already full, and a further review notice joins the unread one found here."
  }
  index "mail_from_agent_id" {
    columns = [column.from_agent_id, column.to_agent_id]
    comment = "Counts the mail two agents have exchanged. A send is refused once that count reaches the cap."
  }
  index "mail_reply_to" {
    columns = [column.reply_to]
  }
}
