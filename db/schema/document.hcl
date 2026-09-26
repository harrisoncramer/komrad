enum "document_kind" {
  schema = schema.public
  values = ["rfc", "report", "explainer", "notes"]
}

table "documents" {
  schema  = schema.public
  comment = "A markdown document an agent wrote for a person to read, kept after its worktree is gone."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "agent_id" {
    null = false
    type = uuid
  }
  column "name" {
    null    = false
    type    = text
    comment = "The title a person reads, unique among one agent's documents."
  }
  column "kind" {
    null = false
    type = enum.document_kind
  }
  column "body" {
    null = false
    type = text
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
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "documents_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  unique "documents_agent_id_name_key" {
    columns = [column.engine_id, column.agent_id, column.name]
  }
  foreign_key "documents_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "documents_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
}
