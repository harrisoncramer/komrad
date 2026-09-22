-- Create enum type "agent_state"
CREATE TYPE "agent_state" AS ENUM ('idle', 'thinking', 'waiting', 'setting_up', 'failed_to_create', 'failed_to_cleanup', 'uncertain');
-- Create enum type "agent_role"
CREATE TYPE "agent_role" AS ENUM ('implementer', 'reviewer', 'orchestrator');
-- Create "workflow_provisioner_configs" table
CREATE TABLE "workflow_provisioner_configs" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "worktree_enabled" boolean NOT NULL DEFAULT false,
  "worktree_base_branch" text NOT NULL,
  "worktree_copy_paths" text[] NOT NULL DEFAULT '{}',
  "worktree_symlink_paths" text[] NOT NULL DEFAULT '{}',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "worktree_diff_exclude" text[] NOT NULL DEFAULT '{}',
  "worktree_dir" text NOT NULL DEFAULT '',
  PRIMARY KEY ("id"),
  CONSTRAINT "worktree_base_branch_named" CHECK (worktree_base_branch <> ''::text)
);
-- Set comment to table: "workflow_provisioner_configs"
COMMENT ON TABLE "workflow_provisioner_configs" IS 'Whether a workflow gives each of its agents a git worktree of its own, and how that worktree is set up.';
-- Set comment to column: "worktree_base_branch" on table: "workflow_provisioner_configs"
COMMENT ON COLUMN "workflow_provisioner_configs"."worktree_base_branch" IS 'The branch worktrees are cut from and every agent''s work is measured against, never blank.';
-- Create "worktree_sizes" table
CREATE TABLE "worktree_sizes" (
  "dir" text NOT NULL,
  "size_bytes" bigint NOT NULL DEFAULT 0,
  "measured_at" timestamptz NOT NULL,
  "error" text NULL,
  PRIMARY KEY ("dir")
);
-- Set comment to table: "worktree_sizes"
COMMENT ON TABLE "worktree_sizes" IS 'The disk one worktree directory takes, as the size loop or get_worktree last measured it. Keyed by the directory rather than an agent, since a directory outlives its row and one nobody answers for is what the worktrees screen is for.';
-- Set comment to column: "size_bytes" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."size_bytes" IS 'Blocks taken rather than length, a hard linked file counted once. Zero while error is set.';
-- Set comment to column: "measured_at" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."measured_at" IS 'When the walk last finished, whether or not it succeeded, so a failed directory is not walked again on every pass.';
-- Set comment to column: "error" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."error" IS 'Why the last walk failed, null when it did not.';
-- Create "workflow_configs" table
CREATE TABLE "workflow_configs" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "harness" text NOT NULL DEFAULT '',
  "cwd" text NOT NULL DEFAULT '',
  "flags" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Set comment to table: "workflow_configs"
COMMENT ON TABLE "workflow_configs" IS 'The harness, working directory and flags a workflow gives each new agent it creates.';
-- Create "workflows" table
CREATE TABLE "workflows" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "name" text NOT NULL,
  "context" text NOT NULL DEFAULT '',
  "workflow_config_id" uuid NOT NULL,
  "provisioner_config_id" uuid NOT NULL,
  "archived_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleting_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "workflows_provisioner_config_id_fkey" FOREIGN KEY ("provisioner_config_id") REFERENCES "workflow_provisioner_configs" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "workflows_workflow_config_id_fkey" FOREIGN KEY ("workflow_config_id") REFERENCES "workflow_configs" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "workflows_provisioner_config_id" to table: "workflows"
CREATE INDEX "workflows_provisioner_config_id" ON "workflows" ("provisioner_config_id");
-- Create index "workflows_workflow_config_id" to table: "workflows"
CREATE INDEX "workflows_workflow_config_id" ON "workflows" ("workflow_config_id");
-- Set comment to table: "workflows"
COMMENT ON TABLE "workflows" IS 'A named group of related agents. It is grouping only. Nothing here runs.';
-- Set comment to column: "deleting_at" on table: "workflows"
COMMENT ON COLUMN "workflows"."deleting_at" IS 'When a delete claimed the workflow, which happens when a delete claims the agents inside it. A claimed workflow is left out of the archive until the last of those agents goes and takes the row with it, and an agent whose delete fails gives the workflow back as it comes back itself.';
-- Create "schedules" table
CREATE TABLE "schedules" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "name" text NOT NULL,
  "cron" text NOT NULL,
  "timezone" text NOT NULL DEFAULT '',
  "enabled" boolean NOT NULL DEFAULT true,
  "hide_agents" boolean NOT NULL DEFAULT false,
  "prompt" text NOT NULL DEFAULT '',
  "workflow_id" uuid NOT NULL,
  "archived_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "last_result_status" text NULL,
  "last_result_message" text NULL,
  "last_result_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "schedules_workflow_id_fkey" FOREIGN KEY ("workflow_id") REFERENCES "workflows" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "schedules_name_active" to table: "schedules"
CREATE UNIQUE INDEX "schedules_name_active" ON "schedules" ("name") WHERE (archived_at IS NULL);
-- Create index "schedules_workflow_id" to table: "schedules"
CREATE INDEX "schedules_workflow_id" ON "schedules" ("workflow_id");
-- Set comment to table: "schedules"
COMMENT ON TABLE "schedules" IS 'A recurring agent launch, holding when to fire and the prompt to launch with.';
-- Create "agents" table
CREATE TABLE "agents" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "name" text NOT NULL,
  "cwd" text NOT NULL DEFAULT '',
  "flags" text NOT NULL DEFAULT '',
  "prompt" text NOT NULL DEFAULT '',
  "pane_id" text NOT NULL DEFAULT '',
  "state" "agent_state" NOT NULL DEFAULT 'idle',
  "state_changed_at" timestamptz NOT NULL DEFAULT now(),
  "harness" text NOT NULL DEFAULT 'claude',
  "session_id" text NOT NULL DEFAULT '',
  "workflow_id" uuid NOT NULL,
  "schedule_id" uuid NULL,
  "idempotency_key" text NULL,
  "spawned_by_agent_id" uuid NULL,
  "archived_at" timestamptz NULL,
  "is_hidden" boolean NOT NULL DEFAULT false,
  "nudged_at" timestamptz NULL,
  "branch" text NULL,
  "worktree_removed" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "state_seen_at" timestamptz NOT NULL DEFAULT now(),
  "interrupted_at" timestamptz NULL,
  "adopted_branch" text NOT NULL DEFAULT '',
  "parent_agent_id" uuid NULL,
  "turn_ended_at" timestamptz NULL,
  "provision_enabled" boolean NULL,
  "reported_reason" text NOT NULL DEFAULT '',
  "role" "agent_role" NOT NULL DEFAULT 'implementer',
  "held_at" timestamptz NULL,
  "held_mid_turn" boolean NOT NULL DEFAULT false,
  "nudge_sent_at" timestamptz NULL,
  "sequence_in_progress" boolean NOT NULL DEFAULT false,
  "repository_url" text NULL,
  "deleting_at" timestamptz NULL,
  "alive" boolean NOT NULL DEFAULT false,
  "tree_standing" text NOT NULL DEFAULT '',
  "tree_standing_sha" text NOT NULL DEFAULT '',
  PRIMARY KEY ("id"),
  CONSTRAINT "agents_parent_agent_id_fkey" FOREIGN KEY ("parent_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agents_schedule_id_fkey" FOREIGN KEY ("schedule_id") REFERENCES "schedules" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "agents_spawned_by_agent_id_fkey" FOREIGN KEY ("spawned_by_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "agents_workflow_id_fkey" FOREIGN KEY ("workflow_id") REFERENCES "workflows" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "agents_idempotency_key" to table: "agents"
CREATE UNIQUE INDEX "agents_idempotency_key" ON "agents" ("idempotency_key") WHERE (idempotency_key IS NOT NULL);
-- Create index "agents_name_active" to table: "agents"
CREATE UNIQUE INDEX "agents_name_active" ON "agents" ("workflow_id", "name") WHERE (archived_at IS NULL);
-- Create index "agents_parent_agent_id" to table: "agents"
CREATE INDEX "agents_parent_agent_id" ON "agents" ("parent_agent_id");
-- Create index "agents_schedule_id" to table: "agents"
CREATE INDEX "agents_schedule_id" ON "agents" ("schedule_id");
-- Create index "agents_workflow_id" to table: "agents"
CREATE INDEX "agents_workflow_id" ON "agents" ("workflow_id");
-- Create index "agents_workflow_orchestrator" to table: "agents"
CREATE UNIQUE INDEX "agents_workflow_orchestrator" ON "agents" ("workflow_id") WHERE (role = 'orchestrator'::agent_role);
-- Set comment to table: "agents"
COMMENT ON TABLE "agents" IS 'One background agent, holding everything needed to start it again.';
-- Set comment to column: "state" on table: "agents"
COMMENT ON COLUMN "agents"."state" IS 'What the agent is doing. Weave captures the agent''s terminal every couple of seconds and writes here what it read there. Uncertain means the terminal was captured but could not be understood. The setup and failure values never come from reading a terminal, so something else sets those.';
-- Set comment to column: "reported_reason" on table: "agents"
COMMENT ON COLUMN "agents"."reported_reason" IS 'What the agent''s own CLI last said it was doing. It only decorates the state read off the terminal and never sets that state. It is cleared whenever the state changes and when a turn ends, so an empty value means there is nothing to say.';
-- Set comment to column: "role" on table: "agents"
COMMENT ON COLUMN "agents"."role" IS 'What the agent was launched to do. It decides which review briefing the agent is given. A reviewer reads the work of the agent it is a child of and never writes any code of its own.';
-- Set comment to column: "held_at" on table: "agents"
COMMENT ON COLUMN "agents"."held_at" IS 'When a person stopped this agent and took the tree, null while nobody is holding it. Held is not a state, since the states are decided by reading the pane and would overwrite it.';
-- Set comment to column: "held_mid_turn" on table: "agents"
COMMENT ON COLUMN "agents"."held_mid_turn" IS 'Whether the hold cut a turn that was still running, which is what decides whether letting go has to type the agent back into motion or merely stop holding it.';
-- Set comment to column: "sequence_in_progress" on table: "agents"
COMMENT ON COLUMN "agents"."sequence_in_progress" IS 'Whether a merge, a rebase or a cherry-pick is part-way through the agent''s checkout and waiting on somebody. Weave reads the checkout every few seconds and writes what it found here, so settling the sequence by hand clears this on its own.';
-- Set comment to column: "repository_url" on table: "agents"
COMMENT ON COLUMN "agents"."repository_url" IS 'The canonical web address of the repository the agent''s checkout pushes to, such as https://github.com/harrisoncramer/weave. Null when the checkout is gone or its origin is not a forge. The cwd syncer rewrites it on the same pass that rewrites branch.';
-- Set comment to column: "deleting_at" on table: "agents"
COMMENT ON COLUMN "agents"."deleting_at" IS 'When a delete claimed the row. A claimed row is left out of every listing until the delete either removes it or fails, and a failure clears the claim as it records itself. The server clears every claim when it starts, since nothing survives a restart to finish one.';
-- Set comment to column: "alive" on table: "agents"
COMMENT ON COLUMN "agents"."alive" IS 'Whether the agent still owns the pane its row names.';
-- Set comment to column: "tree_standing" on table: "agents"
COMMENT ON COLUMN "agents"."tree_standing" IS 'Where the worktree stands against the round its last verdict was given on, empty while it still hashes to it.';
-- Set comment to column: "tree_standing_sha" on table: "agents"
COMMENT ON COLUMN "agents"."tree_standing_sha" IS 'The round the standing was measured against.';
-- Create "agent_metadata" table
CREATE TABLE "agent_metadata" (
  "agent_id" uuid NOT NULL,
  "key" text NOT NULL,
  "value" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("agent_id", "key"),
  CONSTRAINT "agent_metadata_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "agent_metadata"
COMMENT ON TABLE "agent_metadata" IS 'Facts an agent recorded about its own work, such as the pull request it opened.';
-- Create "agent_runs" table
CREATE TABLE "agent_runs" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "agent_id" uuid NOT NULL,
  "trigger" text NOT NULL,
  "script" text NOT NULL,
  "log_number" integer NULL,
  "started_at" timestamptz NOT NULL DEFAULT now(),
  "finished_at" timestamptz NULL,
  "exit_code" integer NULL,
  "failure" text NOT NULL DEFAULT '',
  "viewed_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "agent_runs_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "agent_runs_agent_id_started_at" to table: "agent_runs"
CREATE INDEX "agent_runs_agent_id_started_at" ON "agent_runs" ("agent_id", "started_at" DESC);
-- Create index "agent_runs_unread_failures" to table: "agent_runs"
CREATE INDEX "agent_runs_unread_failures" ON "agent_runs" ("agent_id") WHERE ((failure <> ''::text) AND (viewed_at IS NULL));
-- Set comment to table: "agent_runs"
COMMENT ON TABLE "agent_runs" IS 'One run of a script weave fired for an agent. The output stays on disk and this row says where to find it.';
-- Create "agent_transcript_sessions" table
CREATE TABLE "agent_transcript_sessions" (
  "id" uuid NOT NULL,
  "agent_id" uuid NOT NULL,
  "session_id" text NOT NULL,
  "synced_offset" bigint NOT NULL DEFAULT 0,
  "synced_at" timestamptz NOT NULL DEFAULT now(),
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "agent_transcript_sessions_agent_id_session_id_key" UNIQUE ("agent_id", "session_id"),
  CONSTRAINT "agent_transcript_sessions_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "agent_transcript_sessions"
COMMENT ON TABLE "agent_transcript_sessions" IS 'One conversation an agent has held, holding how far its log has been copied into the store.';
-- Create "agent_transcript_chunks" table
CREATE TABLE "agent_transcript_chunks" (
  "agent_id" uuid NOT NULL,
  "seq" integer NOT NULL,
  "role" text NOT NULL,
  "content" text NOT NULL,
  "content_offset" bigint NOT NULL,
  "session_id" uuid NOT NULL,
  "tsv" tsvector NOT NULL GENERATED ALWAYS AS (to_tsvector('english_nostop'::regconfig, content)) STORED,
  PRIMARY KEY ("agent_id", "seq"),
  CONSTRAINT "agent_transcript_chunks_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_transcript_chunks_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "agent_transcript_sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "agent_transcript_chunks_content_trgm" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_content_trgm" ON "agent_transcript_chunks" USING GIN ("content" gin_trgm_ops);
-- Create index "agent_transcript_chunks_session_id" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_session_id" ON "agent_transcript_chunks" ("session_id");
-- Create index "agent_transcript_chunks_tsv" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_tsv" ON "agent_transcript_chunks" USING GIN ("tsv");
-- Set comment to table: "agent_transcript_chunks"
COMMENT ON TABLE "agent_transcript_chunks" IS 'One turn of an agent''s conversation, indexed so it can be searched by word.';
-- Create "marks" table
CREATE TABLE "marks" (
  "mark" text NOT NULL,
  "agent_id" uuid NULL,
  "workflow_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "id" uuid NOT NULL DEFAULT uuidv7(),
  PRIMARY KEY ("id"),
  CONSTRAINT "marks_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "marks_workflow_id_fkey" FOREIGN KEY ("workflow_id") REFERENCES "workflows" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "marks_one_target" CHECK ((agent_id IS NOT NULL) <> (workflow_id IS NOT NULL))
);
-- Create index "marks_agent_id" to table: "marks"
CREATE UNIQUE INDEX "marks_agent_id" ON "marks" ("agent_id");
-- Create index "marks_mark" to table: "marks"
CREATE UNIQUE INDEX "marks_mark" ON "marks" ("mark");
-- Create index "marks_workflow_id" to table: "marks"
CREATE UNIQUE INDEX "marks_workflow_id" ON "marks" ("workflow_id");
-- Set comment to table: "marks"
COMMENT ON TABLE "marks" IS 'A letter a person types to jump to one row. Each letter belongs to a single agent or a single workflow, so it always names one place.';
-- Create "workflow_hooks" table
CREATE TABLE "workflow_hooks" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "workflow_id" uuid NOT NULL,
  "event" text NOT NULL,
  "script" text NOT NULL,
  "position" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "workflow_hooks_workflow_id_fkey" FOREIGN KEY ("workflow_id") REFERENCES "workflows" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "workflow_hooks_workflow_id_position_idx" to table: "workflow_hooks"
CREATE INDEX "workflow_hooks_workflow_id_position_idx" ON "workflow_hooks" ("workflow_id", "position");
-- Set comment to table: "workflow_hooks"
COMMENT ON TABLE "workflow_hooks" IS 'Scripts a workflow runs at one point in an agent''s life, in position order.';
