enum "worktree_removal_reason" {
  schema = schema.public
  values = ["deleted", "gone"]
}

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
  column "notes" {
    null    = false
    type    = text
    default = ""
    comment = "What the agents on the task and the person have written down about it, as markdown."
  }
  column "notes_updated_at" {
    null    = true
    type    = timestamptz
    comment = "When the notes were last written, null until they first are. A write names the value it read and is refused once it has moved."
  }
  column "cwd" {
    null    = true
    type    = text
    comment = "The directory of the task's checkout, which every agent on the task works in. Null while the task has none."
  }
  column "branch" {
    null    = true
    type    = text
    comment = "The git branch the task's checkout is on, null while it has no checkout or git cannot read it."
  }
  column "adopted_branch" {
    null    = true
    type    = text
    comment = "The branch the task's checkout was put on rather than one komrad made for it, null when komrad made it."
  }
  column "provision_enabled" {
    null    = true
    type    = boolean
    comment = "Whether the task's checkout was provisioned for it: unset inherits the workflow's setting, and false means the checkout is a directory it was given."
  }
  column "worktree_removed" {
    null    = true
    type    = boolean
    comment = "Whether the task's checkout has been taken away, null while it has none."
  }
  column "worktree_removal_reason" {
    null    = true
    type    = enum.worktree_removal_reason
    comment = "Why the task's worktree is no longer on disk: deleted when a delete removed it, gone when komrad found the directory gone without one; null while it is on disk or the task has none."
  }
  column "repository_url" {
    null    = true
    type    = text
    comment = "The canonical web address of the repository the task's checkout pushes to, such as https://github.com/harrisoncramer/komrad. Null when the checkout is gone or its origin is not a forge."
  }
  column "head_sha" {
    null    = true
    type    = text
    comment = "The commit the task's checkout has checked out, null when the checkout is gone or git cannot read it."
  }
  column "last_rebase" {
    null    = true
    type    = jsonb
    comment = "What the latest rebase of the task's checkout came to, as the result or the failure; null until its first rebase."
  }
  column "sequence" {
    null    = true
    type    = enum.agent_sequence
    comment = "What git has part-way through the task's checkout and waiting on somebody, null while it has no checkout."
  }
  column "lines_added" {
    null    = true
    type    = integer
    comment = "How many lines the task's checkout adds since it left its base, committed and uncommitted alike, null when there is no base to measure against."
  }
  column "lines_removed" {
    null    = true
    type    = integer
    comment = "How many lines the task's checkout removes since it left its base, null whenever lines_added is."
  }
  column "moved_since_snapshot" {
    null    = true
    type    = text
    comment = "The commit of the newest review snapshot of the task's agents when its checkout no longer hashes to it, null or empty while it still does or when there is no snapshot."
  }
  column "parent_task_id" {
    null    = true
    type    = uuid
    comment = "The task in the same workflow this task depends on, whose branch this task's branch starts from and is measured against; null for a task on the workflow's base branch."
  }
  column "fork_point" {
    null    = true
    type    = text
    comment = "The fork point, the commit of the parent's branch this task's own commits sit on, which snapshots and line counts measure from; null while the task has no parent."
  }
  column "parent_pull_request_id" {
    null    = true
    type    = uuid
    comment = "The parent's pull request, read for the parent's newest commit and its merge; null until the parent has one or while the task has no parent."
  }
  column "cascade_blocked" {
    null    = true
    type    = text
    comment = "Why the cascade cannot rebase this task onto its parent: conflicts when the rebase stopped on conflicts, orphaned when its branch holds neither its fork point nor the parent's newest commit, closed when the parent's pull request closed without merging; null while nothing blocks it."
  }
  column "is_hidden" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether a person hid the task from the sidebar."
  }
  column "archived_at" {
    null = true
    type = timestamptz
  }
  column "deleting_at" {
    null    = true
    type    = timestamptz
    comment = "When a delete claimed the task, which keeps it out of every listing until it is gone or the delete gives it back."
  }
  column "cleaning_at" {
    null    = true
    type    = timestamptz
    comment = "When the one cleanup that gives back a deleted task's checkout and removes the row took it, so no other cleanup runs it as well."
  }
  column "failure_reason" {
    null    = false
    type    = text
    default = ""
    comment = "Why the task's last delete could not give its checkout back, empty unless one failed."
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
  foreign_key "tasks_parent_task_id_fkey" {
    columns     = [column.engine_id, column.parent_task_id]
    ref_columns = [table.tasks.column.engine_id, table.tasks.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "tasks_parent_pull_request_id_fkey" {
    columns     = [column.engine_id, column.parent_pull_request_id]
    ref_columns = [table.pull_requests.column.engine_id, table.pull_requests.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  check "tasks_slug_kebab" {
    expr = "slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'"
  }
  index "tasks_workflow_id" {
    columns = [column.workflow_id]
  }
  index "tasks_parent_task_id" {
    columns = [column.parent_task_id]
  }
}

table "task_pull_requests" {
  schema  = schema.public
  comment = "Which tasks were seen with which pull requests, one row per pair, never re-pointed. The watch carries every pull request with a row here."
  column "task_id" {
    null = false
    type = uuid
  }
  column "pull_request_id" {
    null = false
    type = uuid
  }
  column "first_seen_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "last_seen_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
    comment = "When the pair was last seen together, moved at most once an hour."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.task_id, column.pull_request_id]
  }
  foreign_key "task_pull_requests_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "task_pull_requests_task_id_fkey" {
    columns     = [column.engine_id, column.task_id]
    ref_columns = [table.tasks.column.engine_id, table.tasks.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "task_pull_requests_pull_request_id_fkey" {
    columns     = [column.engine_id, column.pull_request_id]
    ref_columns = [table.pull_requests.column.engine_id, table.pull_requests.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "task_pull_requests_pull_request_id" {
    columns = [column.pull_request_id]
  }
}
