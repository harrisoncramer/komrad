table "document_types" {
  schema  = schema.public
  comment = "A sort of document a person defines, with the instructions an agent follows when writing one."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "name" {
    null    = false
    type    = text
    comment = "What agents and people call the type, unique within an engine."
  }
  column "context" {
    null    = false
    type    = text
    comment = "The markdown an agent reads before writing a document of this type."
  }
  column "version" {
    null    = false
    type    = integer
    default = 1
    comment = "How many times the type has been written, which a rewrite names to say what it replaces."
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
  unique "document_types_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  unique "document_types_engine_id_name_key" {
    columns = [column.engine_id, column.name]
  }
  foreign_key "document_types_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  check "document_types_name_given" {
    expr = "name <> ''"
  }
}


table "document_type_versions" {
  schema  = schema.public
  comment = "Every write of a document type as it was written, so a person can step back through its context."
  column "document_type_id" {
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
  column "context" {
    null = false
    type = text
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
    columns = [column.document_type_id, column.version]
  }
  foreign_key "document_type_versions_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "document_type_versions_document_type_id_fkey" {
    columns     = [column.engine_id, column.document_type_id]
    ref_columns = [table.document_types.column.engine_id, table.document_types.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  check "document_type_versions_context_written" {
    expr = "context <> ''"
  }
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
  column "type" {
    null    = false
    type    = text
    comment = "The name of the document type it was written as."
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
  foreign_key "documents_type_fkey" {
    columns     = [column.engine_id, column.type]
    ref_columns = [table.document_types.column.engine_id, table.document_types.column.name]
    on_delete   = RESTRICT
    on_update   = CASCADE
  }
  check "documents_body_written" {
    expr = "body <> ''"
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
  column "type" {
    null    = false
    type    = text
    comment = "The name of the document type this version was written as."
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
  check "document_versions_body_written" {
    expr = "body <> ''"
  }
}
