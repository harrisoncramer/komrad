table "tasks" {
  schema  = schema.public
  comment = "One outcome a person wants, which the agents linked to it work toward."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "workflow_id" {
    null = false
    type = uuid
  }
  column "slug" {
    null    = false
    type    = text
    comment = "The lowercase kebab-case name agents and people call the task by, unique within an engine and never reused."
  }
  column "title" {
    null = false
    type = text
  }
  column "goal" {
    null    = false
    type    = text
    default = ""
    comment = "What the task is meant to achieve, as markdown."
  }
  column "archived_at" {
    null = true
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
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "tasks_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  unique "tasks_engine_id_slug_key" {
    columns = [column.engine_id, column.slug]
  }
  foreign_key "tasks_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "tasks_workflow_id_fkey" {
    columns     = [column.engine_id, column.workflow_id]
    ref_columns = [table.workflows.column.engine_id, table.workflows.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  check "tasks_slug_kebab" {
    expr = "slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'"
  }
  index "tasks_workflow_id" {
    columns = [column.workflow_id]
  }
}
