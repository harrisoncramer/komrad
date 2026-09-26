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
  unique "schedules_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "schedules_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "schedules_workflow_id_fkey" {
    columns     = [column.engine_id, column.workflow_id]
    ref_columns = [table.workflows.column.engine_id, table.workflows.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  index "schedules_name_active" {
    unique  = true
    columns = [column.engine_id, column.name]
    where   = "archived_at IS NULL"
  }
  index "schedules_workflow_id" {
    columns = [column.workflow_id]
  }
}

table "schedule_runs" {
  schema  = schema.public
  column "id" {
    null = false
    type = bigint
    identity {
      generated = ALWAYS
    }
  }
  column "schedule_id" {
    null = false
    type = uuid
  }
  column "scheduled_for" {
    null    = false
    type    = timestamptz
  }
  column "state" {
    null    = false
    type    = text
  }
  column "attempt" {
    null = false
    type = integer
  }
  column "max_attempts" {
    null = false
    type = integer
  }
  column "message" {
    null    = false
    type    = text
    default = ""
  }
  column "started_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "finalized_at" {
    null    = true
    type    = timestamptz
  }
  column "engine_id" {
    null    = false
    type    = uuid
  }
  primary_key {
    columns = [column.engine_id, column.id]
  }
  foreign_key "schedule_runs_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "schedule_runs_schedule_id_fkey" {
    columns     = [column.engine_id, column.schedule_id]
    ref_columns = [table.schedules.column.engine_id, table.schedules.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  unique "schedule_runs_occurrence_key" {
    columns = [column.engine_id, column.schedule_id, column.scheduled_for]
  }
  check "schedule_runs_state_known" {
    expr = "state IN ('running', 'retryable', 'completed', 'cancelled', 'discarded')"
  }
}
