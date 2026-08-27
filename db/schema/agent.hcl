schema "public" {
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

table "workflow_action_configs" {
  schema  = schema.public
  comment = "The setup/cleanup actions a workflow's agents run around their lives, and the worktree config those actions can point at instead."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "setup_action" {
    null    = false
    type    = text
    default = ""
  }
  column "cleanup_action" {
    null    = false
    type    = text
    default = ""
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
  column "action_config_id" {
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
  foreign_key "workflows_action_config_id_fkey" {
    columns     = [column.action_config_id]
    ref_columns = [table.workflow_action_configs.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  index "workflows_workflow_config_id" {
    columns = [column.workflow_config_id]
  }
  index "workflows_action_config_id" {
    columns = [column.action_config_id]
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
  column "setup_action" {
    null = true
    type = text
  }
  column "cleanup_action" {
    null = true
    type = text
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
  column "state" {
    null    = false
    type    = text
    default = "idle"
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
  column "worktree_removed" {
    null    = false
    type    = boolean
    default = false
  }
  column "interrupted_at" {
    null = true
    type = timestamptz
  }
  column "tag" {
    null = true
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
  index "agents_idempotency_key" {
    unique  = true
    columns = [column.idempotency_key]
    where   = "idempotency_key IS NOT NULL"
  }
}
