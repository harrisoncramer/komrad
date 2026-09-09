table "schedules" {
  schema  = schema.public
  comment = "A recurring agent launch, holding when to fire and the prompt to launch with."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "name" {
    null = false
    type = text
  }
  column "cron" {
    null = false
    type = text
  }
  column "timezone" {
    null    = false
    type    = text
    default = ""
  }
  column "enabled" {
    null    = false
    type    = boolean
    default = true
  }
  column "hide_agents" {
    null    = false
    type    = boolean
    default = false
  }
  column "prompt" {
    null    = false
    type    = text
    default = ""
  }
  column "workflow_id" {
    null = false
    type = uuid
  }
  column "last_result_status" {
    null = true
    type = text
  }
  column "last_result_message" {
    null = true
    type = text
  }
  column "last_result_at" {
    null = true
    type = timestamptz
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
  primary_key {
    columns = [column.id]
  }
  foreign_key "schedules_workflow_id_fkey" {
    columns     = [column.workflow_id]
    ref_columns = [table.workflows.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  index "schedules_name_active" {
    unique  = true
    columns = [column.name]
    where   = "archived_at IS NULL"
  }
  index "schedules_workflow_id" {
    columns = [column.workflow_id]
  }
}
