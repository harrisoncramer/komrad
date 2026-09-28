enum "mail_kind" {
  schema = schema.public
  values = ["plain", "review_activity", "exchange_capped", "document_edited", "pull_request_changed", "worktree_rebased"]
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
    comment = "Who wrote the mail and why. Plain is mail an agent sent. The other values are notices komrad raised itself, which the panel draws differently. Komrad also counts unread notices by this value. One unread notice of a kind is enough however much is waiting behind it."
  }
  column "subject_id" {
    null    = true
    type    = uuid
    comment = "The thing a komrad notice is about, null on ordinary mail. Its kind says what sort of thing it is: another agent, a document, a pull request or a worktree. A recipient holds at most one unread notice of a kind about one subject, and a later notice rewrites it. There is no foreign key here on purpose. The notice has to keep naming its subject after the subject is gone."
  }
  column "idempotency_key" {
    null    = true
    type    = text
    comment = "The key a retried send is recognised by, null on mail sent once."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "mail_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "mail_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "mail_from_agent_id_fkey" {
    columns     = [column.engine_id, column.from_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "mail_to_agent_id_fkey" {
    columns     = [column.engine_id, column.to_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "mail_reply_to_fkey" {
    columns     = [column.engine_id, column.reply_to]
    ref_columns = [table.mail.column.engine_id, table.mail.column.id]
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
  index "mail_pending_subject" {
    unique  = true
    columns = [column.to_agent_id, column.kind, column.subject_id]
    where   = "claimed_at IS NULL AND subject_id IS NOT NULL"
    comment = "Holds a recipient to one unread notice of a kind about one subject, which a later notice rewrites in place."
  }
  index "mail_idempotency_key" {
    unique  = true
    columns = [column.engine_id, column.idempotency_key]
    where   = "idempotency_key IS NOT NULL"
  }
}
