# A recurring agent launch, holding when to fire and what to ask for. Where and
# how the agent runs is the workflow's to say, so nothing about the environment is
# repeated here.
#
# The row holds no pointer to the agents it has produced, since a schedule fires
# many times over its life. Each agent records the schedule it came from.
table "schedules" {
  schema  = schema.public
  comment = "A cron-driven agent launch: when to fire and what to ask for."
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
  # The workflow this schedule launches into, and the reason a schedule holds no
  # directory or flags of its own. It reads them off the workflow when it fires, so
  # a workflow that moves takes its schedules with it. References workflows.id.
  column "workflow_id" {
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
  # A schedule reads its directory and flags off the workflow every time it
  # fires, so one pointing at a workflow that does not exist could never launch
  # anything. Workflows are only soft-deleted, so this never blocks a delete.
  foreign_key "schedules_workflow_id_fkey" {
    columns     = [column.workflow_id]
    ref_columns = [table.workflows.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
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
