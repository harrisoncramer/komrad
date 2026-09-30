schema "public" {
}

enum "agent_state" {
  schema = schema.public
  values = ["idle", "thinking", "waiting", "setting_up", "failed_to_create", "failed_to_cleanup", "uncertain", "errored"]
}

enum "agent_role" {
  schema = schema.public
  values = ["implementer", "reviewer", "orchestrator"]
}

table "workflow_configs" {
  schema  = schema.public
  comment = "The harness, working directory and flags a workflow gives each new agent it creates."
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
  column "reviewer_harness" {
    null    = false
    type    = text
    default = ""
    comment = "The harness the workflow's reviewers run, blank meaning the one its other agents run."
  }
  column "reviewer_flags" {
    null    = false
    type    = text
    default = ""
    comment = "The flags the workflow's reviewers are launched with, blank meaning the other agents' flags when the harness is the same."
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
  unique "workflow_configs_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "workflow_configs_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
}

table "workflow_provisioner_configs" {
  schema  = schema.public
  comment = "Whether a workflow gives each of its agents a git worktree of its own, and how that worktree is set up."
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
    comment = "The branch worktrees are cut from and every agent's work is measured against, never blank."
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
  column "worktree_diff_exclude" {
    null    = false
    type    = sql("text[]")
    default = sql("'{}'")
  }
  column "worktree_dir" {
    null    = false
    type    = text
    default = ""
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "workflow_provisioner_configs_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "workflow_provisioner_configs_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  check "worktree_base_branch_named" {
    expr = "worktree_base_branch <> ''"
  }
}

table "workflow_hooks" {
  schema  = schema.public
  comment = "Scripts a workflow runs at one point in an agent's life, in position order."
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
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  foreign_key "workflow_hooks_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "workflow_hooks_workflow_id_fkey" {
    columns     = [column.engine_id, column.workflow_id]
    ref_columns = [table.workflows.column.engine_id, table.workflows.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "workflow_hooks_workflow_id_position_idx" {
    columns = [column.workflow_id, column.position]
  }
}

table "workflows" {
  schema  = schema.public
  comment = "A named group of related agents. It is grouping only. Nothing here runs."
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
  column "deleting_at" {
    null    = true
    type    = timestamptz
    comment = "When a delete claimed the workflow, which happens when a delete claims the agents inside it. A claimed workflow is left out of the archive until the last of those agents goes and takes the row with it, and an agent whose delete fails gives the workflow back as it comes back itself."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "workflows_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "workflows_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "workflows_workflow_config_id_fkey" {
    columns     = [column.engine_id, column.workflow_config_id]
    ref_columns = [table.workflow_configs.column.engine_id, table.workflow_configs.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "workflows_provisioner_config_id_fkey" {
    columns     = [column.engine_id, column.provisioner_config_id]
    ref_columns = [table.workflow_provisioner_configs.column.engine_id, table.workflow_provisioner_configs.column.id]
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

table "agent_transcript_sessions" {
  schema  = schema.public
  comment = "One conversation an agent has held, holding how far its log has been copied into the store."
  column "id" {
    null = false
    type = uuid
  }
  column "agent_id" {
    null = false
    type = uuid
  }
  column "session_id" {
    null = false
    type = text
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
  column "created_at" {
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
  unique "agent_transcript_sessions_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "agent_transcript_sessions_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "agent_transcript_sessions_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  unique "agent_transcript_sessions_agent_id_session_id_key" {
    columns = [column.agent_id, column.session_id]
  }
}

table "agent_transcript_chunks" {
  schema  = schema.public
  comment = "One turn of an agent's conversation, indexed so it can be searched by word."
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
  column "session_id" {
    null = false
    type = uuid
  }
  column "tsv" {
    null = false
    type = sql("tsvector")
    as {
      expr = "to_tsvector('english_nostop', content)"
      type = STORED
    }
  }
  column "spoken_at" {
    null    = true
    type    = timestamptz
    comment = "When the turn was written, read from its line in the log, null for a turn whose log was gone before it was read with times."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.agent_id, column.seq]
  }
  foreign_key "agent_transcript_chunks_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "agent_transcript_chunks_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "agent_transcript_chunks_session_id_fkey" {
    columns     = [column.engine_id, column.session_id]
    ref_columns = [table.agent_transcript_sessions.column.engine_id, table.agent_transcript_sessions.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "agent_transcript_chunks_session_id" {
    columns = [column.session_id]
  }
  index "agent_transcript_chunks_spoken_at" {
    columns = [column.spoken_at]
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

table "agent_screens" {
  schema  = schema.public
  comment = "The last screen an agent's pane drew while it was at rest, kept so the panel can show it once the pane is gone."
  column "agent_id" {
    null = false
    type = uuid
  }
  column "frame" {
    null    = false
    type    = text
    comment = "The pane's visible grid as tmux captured it, colour escapes included."
  }
  column "width" {
    null = false
    type = integer
  }
  column "height" {
    null = false
    type = integer
  }
  column "captured_at" {
    null = false
    type = timestamptz
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.agent_id]
  }
  foreign_key "agent_screens_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "agent_screens_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  check "agent_screens_sized" {
    expr = "width > 0 AND height > 0"
  }
}

table "agent_runs" {
  schema  = schema.public
  comment = "One run of a script komrad fired for an agent. The output stays on disk and this row says where to find it."
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
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  foreign_key "agent_runs_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "agent_runs_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
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
  comment = "One background agent, holding everything needed to start it again."
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
    type    = enum.agent_state
    default = "idle"
    comment = "What the agent is doing. Komrad captures the agent's terminal every couple of seconds and writes here what it read there. Uncertain means the terminal was captured but could not be understood. Errored means the terminal is at rest after an API error ended the last turn. The setup and failure values never come from reading a terminal, so something else sets those."
  }
  column "state_changed_at" {
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
  column "archive_batch_id" {
    null    = true
    type    = uuid
    comment = "The archive this agent was taken out of live listings by, shared by every agent that archive took."
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
  column "state_seen_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "interrupted_at" {
    null = true
    type = timestamptz
  }
  column "adopted_branch" {
    null    = false
    type    = text
    default = ""
  }
  column "parent_agent_id" {
    null = true
    type = uuid
  }
  column "turn_ended_at" {
    null = true
    type = timestamptz
  }
  column "turn_failed_at" {
    null    = true
    type    = timestamptz
    comment = "When an API error last ended one of the agent's turns. The last turn failed while this is no older than turn_ended_at."
  }
  column "turn_error" {
    null    = false
    type    = text
    default = ""
    comment = "What the agent's CLI printed when an API error last ended one of its turns."
  }
  column "provision_enabled" {
    null = true
    type = boolean
  }
  column "reported_reason" {
    null    = false
    type    = text
    default = ""
    comment = "What the agent's own CLI last said it was doing. It only decorates the state read off the terminal and never sets that state. It is cleared whenever the state changes and when a turn ends, so an empty value means there is nothing to say."
  }
  column "role" {
    null    = false
    type    = enum.agent_role
    default = "implementer"
    comment = "What the agent was launched to do. It decides which review briefing the agent is given. A reviewer reads the work of the agent it is a child of and never writes any code of its own."
  }
  column "failure_reason" {
    null    = false
    type    = text
    default = ""
    comment = "Why the agent's create or cleanup failed, empty unless it is in one of the failed states."
  }
  column "held_at" {
    null    = true
    type    = timestamptz
    comment = "When a person stopped this agent and took the tree, null while nobody is holding it. Held is not a state, since the states are decided by reading the pane and would overwrite it."
  }
  column "held_mid_turn" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether the hold cut a turn that was still running, which is what decides whether letting go has to type the agent back into motion or merely stop holding it."
  }
  column "nudge_sent_at" {
    null = true
    type = timestamptz
  }
  column "sequence_in_progress" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether a merge, a rebase or a cherry-pick is part-way through the agent's checkout and waiting on somebody. Komrad reads the checkout every few seconds and writes what it found here, so settling the sequence by hand clears this on its own."
  }
  column "last_rebase" {
    null    = true
    type    = jsonb
    comment = "What the latest rebase of the agent's checkout came to, as the result or the failure; null until its first rebase."
  }
  column "head_sha" {
    null    = true
    type    = text
    comment = "The commit the agent's checkout has checked out, null when the checkout is gone or git cannot read it. The cwd syncer rewrites it on the same pass that rewrites branch."
  }
  column "repository_url" {
    null    = true
    type    = text
    comment = "The canonical web address of the repository the agent's checkout pushes to, such as https://github.com/harrisoncramer/komrad. Null when the checkout is gone or its origin is not a forge. The cwd syncer rewrites it on the same pass that rewrites branch."
  }
  column "deleting_at" {
    null    = true
    type    = timestamptz
    comment = "When a delete claimed the row. A claimed row is left out of every listing until the delete either removes it or fails, and a failure clears the claim as it records itself. The server clears every claim when it starts, since nothing survives a restart to finish one."
  }
  column "alive" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether the agent still owns the pane its row names."
  }
  column "moved_since_round" {
    null    = false
    type    = text
    default = ""
    comment = "The snapshot of the agent's newest review round when its worktree no longer hashes to it, empty while it still does or when there is no round. The cwd syncer rewrites it every pass."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "agents_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "agents_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "agents_workflow_id_fkey" {
    columns     = [column.engine_id, column.workflow_id]
    ref_columns = [table.workflows.column.engine_id, table.workflows.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "agents_schedule_id_fkey" {
    columns     = [column.engine_id, column.schedule_id]
    ref_columns = [table.schedules.column.engine_id, table.schedules.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "agents_spawned_by_agent_id_fkey" {
    columns     = [column.engine_id, column.spawned_by_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = SET_NULL
    on_update   = NO_ACTION
  }
  foreign_key "agents_parent_agent_id_fkey" {
    columns     = [column.engine_id, column.parent_agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "agents_name_active" {
    unique  = true
    columns = [column.workflow_id, column.name]
    where   = "archived_at IS NULL"
  }
  index "agents_workflow_orchestrator" {
    unique  = true
    columns = [column.workflow_id]
    where   = "role = 'orchestrator'"
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
    columns = [column.engine_id, column.idempotency_key]
    where   = "idempotency_key IS NOT NULL"
  }
}
