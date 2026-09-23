table "tenants" {
  schema  = schema.public
  comment = "An owner of rows, every other table carrying the id of the tenant it belongs to."
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
