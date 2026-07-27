# A schedule's launch configuration, split out of schedules so it is a
# first-class record. Strict 1:1 with a schedule.
table "schedule_configs" {
  schema  = schema.public
  comment = "Launch config (cwd, flags) owned by exactly one schedule."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "cwd" {
    null    = false
    type    = text
    default = ""
  }
  column "flags" {
    null    = false
    type    = text
    default = ""
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
}

# A recurring agent launch. The row is the blueprint: what to run, where, and
# how often. It holds no pointer to the agents it has produced, since a schedule
# fires many times over its life; each agent records the schedule it came from.
table "schedules" {
  schema  = schema.public
  comment = "A cron-driven agent launch: what to run, where, and how often."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "name" {
    null = false
    type = text
  }
  # A standard five-field cron expression, parsed by the scheduler rather than
  # by the database. Postgres stores it as the text the user wrote so an invalid
  # one is rejected at the validation layer.
  column "cron" {
    null = false
    type = text
  }
  # IANA zone the cron expression is read in, empty for the machine's local
  # zone. A zone rather than a fixed offset, so a 9am schedule stays at 9am
  # across a daylight saving change.
  column "timezone" {
    null    = false
    type    = text
    default = ""
  }
  # Paused schedules keep their row and their history and simply stop firing.
  # Distinct from is_deleted: pausing is a state the user toggles, deleting is
  # the end of the schedule.
  column "enabled" {
    null    = false
    type    = boolean
    default = true
  }
  # The prompt each launched agent starts with. This is the whole point of the
  # schedule, so unlike an agent's prompt it is expected to be set.
  column "prompt" {
    null    = false
    type    = text
    default = ""
  }
  # The workflow launched agents are filed under, or null to leave them at the
  # root. Null, not an empty sentinel, matching agents.workflow_id. References
  # workflows.id.
  column "workflow_id" {
    null = true
    type = uuid
  }
  # This schedule's own config. Set on insert and never null: a schedule always owns exactly one config row.
  column "schedule_config_id" {
    null = false
    type = uuid
  }
  # Soft-delete flag: a deleted schedule is hidden from the panel but kept, so
  # the agents it produced still resolve to the schedule that launched them.
  column "is_deleted" {
    null    = false
    type    = boolean
    default = false
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
  # Unique on name only among live records, the same predicate agents use. A
  # schedule's name seeds the names of the agents it launches.
  index "schedules_name_active" {
    unique  = true
    columns = [column.name]
    where   = "is_deleted = false"
  }
  # Scoped listings filter by workflow, so index the foreign key.
  index "schedules_workflow_id" {
    columns = [column.workflow_id]
  }
}
