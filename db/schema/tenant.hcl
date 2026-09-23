table "tenants" {
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
  unique "tenants_name_key" {
    columns = [column.name]
  }
}

table "tenant_logins" {
  schema  = schema.public
  comment = "The Postgres logins an engine connects as, which is the only way an engine's identity is known."
  column "role" {
    null = false
    type = text
  }
  column "tenant_id" {
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
  foreign_key "tenant_logins_tenant_id_fkey" {
    columns     = [column.tenant_id]
    ref_columns = [table.tenants.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "tenant_logins_tenant_id" {
    columns = [column.tenant_id]
  }
}
