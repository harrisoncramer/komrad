schema "public" {
}

enum "agent_state" {
  schema = schema.public
  values = ["idle", "thinking", "waiting", "setting_up", "failed_to_create", "failed_to_cleanup", "uncertain"]
}

enum "agent_role" {
  schema = schema.public
  values = ["implementer", "reviewer"]
}

table "workflow_configs" {
  schema  = schema.public
  comment = "Default config (harness, cwd, flags) seeded into new agents of a workflow."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "harness" {
    null    = false
    type    = text
    default = ""
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

table "workflow_provisioner_configs" {
  schema  = schema.public
  comment = "The worktree config a workflow provisions its agents with, and whether it provisions them at all."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "worktree_enabled" {
    null    = false
    type    = boolean
    default = false
  }
  column "worktree_base_branch" {
    null    = false
    type    = text
    default = ""
  }
  column "worktree_dir" {
    null    = false
    type    = text
    default = ""
  }
  column "worktree_copy_paths" {
    null    = false
    type    = sql("text[]")
    default = sql("'{}'")
  }
  column "worktree_symlink_paths" {
    null    = false
    type    = sql("text[]")
    default = sql("'{}'")
  }
  column "worktree_diff_exclude" {
    null    = false
    type    = sql("text[]")
    default = sql("'{}'")
  }
  column "archive_on_pr_closed" {
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
}

table "workflow_hooks" {
  schema  = schema.public
  comment = "The scripts a workflow hangs on one moment in its agents' lives, run in position order."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "workflow_id" {
    null = false
    type = uuid
  }
  column "event" {
    null = false
    type = text
  }
  column "script" {
    null = false
    type = text
  }
  column "position" {
    null = false
    type = integer
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  primary_key {
    columns = [column.id]
  }
  foreign_key "workflow_hooks_workflow_id_fkey" {
    columns     = [column.workflow_id]
    ref_columns = [table.workflows.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "workflow_hooks_workflow_id_position_idx" {
    columns = [column.workflow_id, column.position]
  }
}

table "workflows" {
  schema  = schema.public
  comment = "A named bucket of related agents. Pure organization; runs nothing."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "name" {
    null = false
    type = text
  }
  column "context" {
    null    = false
    type    = text
    default = ""
  }
  column "workflow_config_id" {
    null = false
    type = uuid
  }
  column "provisioner_config_id" {
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
  primary_key {
    columns = [column.id]
  }
  foreign_key "workflows_workflow_config_id_fkey" {
    columns     = [column.workflow_config_id]
    ref_columns = [table.workflow_configs.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "workflows_provisioner_config_id_fkey" {
    columns     = [column.provisioner_config_id]
    ref_columns = [table.workflow_provisioner_configs.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  index "workflows_workflow_config_id" {
    columns = [column.workflow_config_id]
  }
  index "workflows_provisioner_config_id" {
    columns = [column.provisioner_config_id]
  }
}

table "agent_transcripts" {
  schema  = schema.public
  comment = "Flattened transcript for an agent, synced from Claude's on-disk JSONL and indexed for search. Strict 1:1 with an agent."
  column "agent_id" {
    null = false
    type = uuid
  }
  column "content" {
    null    = false
    type    = text
    default = ""
  }
  column "synced_offset" {
    null    = false
    type    = bigint
    default = 0
  }
  column "synced_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  primary_key {
    columns = [column.agent_id]
  }
  foreign_key "agent_transcripts_agent_id_fkey" {
    columns     = [column.agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
}

table "agent_transcript_chunks" {
  schema  = schema.public
  comment = "One turn of an agent's transcript, word-indexed for search."
  column "agent_id" {
    null = false
    type = uuid
  }
  column "seq" {
    null = false
    type = int
  }
  column "role" {
    null = false
    type = text
  }
  column "content" {
    null = false
    type = text
  }
  column "content_offset" {
    null = false
    type = bigint
  }
  column "tsv" {
    null = false
    type = sql("tsvector")
    as {
      expr = "to_tsvector('english', content)"
      type = STORED
    }
  }
  primary_key {
    columns = [column.agent_id, column.seq]
  }
  foreign_key "agent_transcript_chunks_agent_id_fkey" {
    columns     = [column.agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "agent_transcript_chunks_tsv" {
    type    = GIN
    columns = [column.tsv]
  }
  index "agent_transcript_chunks_content_trgm" {
    type = GIN
    on {
      column = column.content
      ops    = gin_trgm_ops
    }
  }
}

table "agent_metadata" {
  schema  = schema.public
  comment = "Key/value facts an agent recorded about its own work."
  column "agent_id" {
    null = false
    type = uuid
  }
  column "key" {
    null = false
    type = text
  }
  column "value" {
    null = false
    type = text
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
    columns = [column.agent_id, column.key]
  }
  foreign_key "agent_metadata_agent_id_fkey" {
    columns     = [column.agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
}

table "agent_runs" {
  schema  = schema.public
  comment = "One run of a script weave fired for an agent, indexing the output it left on disk."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "agent_id" {
    null = false
    type = uuid
  }
  column "trigger" {
    null = false
    type = text
  }
  column "script" {
    null = false
    type = text
  }
  column "log_number" {
    null = true
    type = integer
  }
  column "started_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "finished_at" {
    null = true
    type = timestamptz
  }
  column "exit_code" {
    null = true
    type = integer
  }
  column "failure" {
    null    = false
    type    = text
    default = ""
  }
  column "viewed_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  foreign_key "agent_runs_agent_id_fkey" {
    columns     = [column.agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "agent_runs_agent_id_started_at" {
    on {
      column = column.agent_id
    }
    on {
      column = column.started_at
      desc   = true
    }
  }
  index "agent_runs_unread_failures" {
    columns = [column.agent_id]
    where   = "failure <> '' AND viewed_at IS NULL"
  }
}

table "agents" {
  schema  = schema.public
  comment = "One background Claude agent, everything needed to restore it."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "name" {
    null = false
    type = text
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
  column "provision_enabled" {
    null = true
    type = boolean
  }
  column "prompt" {
    null    = false
    type    = text
    default = ""
  }
  column "pane_id" {
    null    = false
    type    = text
    default = ""
  }
  column "role" {
    null    = false
    type    = enum.agent_role
    default = "implementer"
    comment = "What the agent was launched to do, which decides the half of the review briefing it is sent. A reviewer reads the work of the agent it is a child of and never writes any of its own."
  }
  column "state" {
    null    = false
    type    = enum.agent_state
    default = "idle"
    comment = "What the agent is doing, which the reconcile loop writes from what it read off the pane. Uncertain is a pane weave captured but could not read. The three failure and setup values are the ones no frame read ever decides."
  }
  column "state_changed_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "state_seen_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "viewed_at" {
    null = true
    type = timestamptz
  }
  column "turn_ended_at" {
    null = true
    type = timestamptz
  }
  column "reported_reason" {
    null    = false
    type    = text
    default = ""
    comment = "What the agent's own CLI last reported it was doing, which annotates the state the pane decided rather than deciding one of its own. Cleared with the state it annotates whenever that changes, and by a turn ending, so empty means there is nothing to say."
  }
  column "harness" {
    null    = false
    type    = text
    default = "claude"
  }
  column "session_id" {
    null    = false
    type    = text
    default = ""
  }
  column "workflow_id" {
    null = false
    type = uuid
  }
  column "schedule_id" {
    null = true
    type = uuid
  }
  column "idempotency_key" {
    null = true
    type = text
  }
  column "spawned_by_agent_id" {
    null = true
    type = uuid
  }
  column "parent_agent_id" {
    null = true
    type = uuid
  }
  column "archived_at" {
    null = true
    type = timestamptz
  }
  column "is_hidden" {
    null    = false
    type    = boolean
    default = false
  }
  column "nudged_at" {
    null = true
    type = timestamptz
  }
  column "branch" {
    null = true
    type = text
  }
  column "adopted_branch" {
    null    = false
    type    = text
    default = ""
  }
  column "worktree_removed" {
    null    = false
    type    = boolean
    default = false
  }
  column "interrupted_at" {
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
  foreign_key "agents_workflow_id_fkey" {
    columns     = [column.workflow_id]
    ref_columns = [table.workflows.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "agents_schedule_id_fkey" {
    columns     = [column.schedule_id]
    ref_columns = [table.schedules.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "agents_spawned_by_agent_id_fkey" {
    columns     = [column.spawned_by_agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "agents_parent_agent_id_fkey" {
    columns     = [column.parent_agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "agents_name_active" {
    unique  = true
    columns = [column.workflow_id, column.name]
    where   = "archived_at IS NULL"
  }
  index "agents_workflow_id" {
    columns = [column.workflow_id]
  }
  index "agents_schedule_id" {
    columns = [column.schedule_id]
  }
  index "agents_parent_agent_id" {
    columns = [column.parent_agent_id]
  }
  index "agents_idempotency_key" {
    unique  = true
    columns = [column.idempotency_key]
    where   = "idempotency_key IS NOT NULL"
  }
}

table "marks" {
  schema  = schema.public
  comment = "A letter a person addresses one row by. Held by an agent or a workflow and by nothing else, so a letter names one place."
  column "mark" {
    null = false
    type = text
  }
  column "agent_id" {
    null = true
    type = uuid
  }
  column "workflow_id" {
    null = true
    type = uuid
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  primary_key {
    columns = [column.mark]
  }
  foreign_key "marks_agent_id_fkey" {
    columns     = [column.agent_id]
    ref_columns = [table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "marks_workflow_id_fkey" {
    columns     = [column.workflow_id]
    ref_columns = [table.workflows.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "marks_agent_id" {
    unique  = true
    columns = [column.agent_id]
  }
  index "marks_workflow_id" {
    unique  = true
    columns = [column.workflow_id]
  }
  check "marks_one_target" {
    expr = "(agent_id IS NOT NULL) <> (workflow_id IS NOT NULL)"
  }
}
