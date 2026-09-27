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
  column "version" {
    null    = false
    type    = integer
    default = 1
    comment = "How many times the document has been written, which a rewrite names to say what it replaces."
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

table "document_versions" {
  schema  = schema.public
  comment = "Every write of a document as it was written, so a person can step back through it."
  column "document_id" {
    null = false
    type = uuid
  }
  column "version" {
    null = false
    type = integer
  }
  column "name" {
    null = false
    type = text
  }
  column "kind" {
    null = false
    type = enum.document_kind
  }
  column "body" {
    null = false
    type = text
  }
  column "by_person" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether a person wrote this version in the panel rather than the agent that owns the document."
  }
  column "created_at" {
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
    columns = [column.document_id, column.version]
  }
  foreign_key "document_versions_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "document_versions_document_id_fkey" {
    columns     = [column.engine_id, column.document_id]
    ref_columns = [table.documents.column.engine_id, table.documents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
}
