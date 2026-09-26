table "engines" {
  schema  = schema.public
  comment = "An engine, the owner of every row it writes."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "name" {
    null = false
    type = text
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  primary_key {
    columns = [column.id]
  }
  unique "engines_name_key" {
    columns = [column.name]
  }
}

table "engine_logins" {
  schema  = schema.public
  comment = "The Postgres logins an engine connects as, which is the only way an engine's identity is known."
  column "role" {
    null = false
    type = text
  }
  column "engine_id" {
    null = false
    type = uuid
  }
  column "reader" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether this is the engine's read-only login rather than the one it writes with."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  primary_key {
    columns = [column.role]
  }
  foreign_key "engine_logins_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "engine_logins_engine_id" {
    columns = [column.engine_id]
  }
}
