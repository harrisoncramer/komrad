-- pg_trgm backs the transcript search index below.
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

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
COMMENT ON TABLE "workflow_configs" IS 'Default config (harness, cwd, flags) seeded into new agents of a workflow.';
-- Create "workflows" table
CREATE TABLE "workflows" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "name" text NOT NULL,
  "context" text NOT NULL DEFAULT '',
  "setup_action" text NOT NULL DEFAULT '',
  "cleanup_action" text NOT NULL DEFAULT '',
  "workflow_config_id" uuid NOT NULL,
  "archived_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "workflows_workflow_config_id_fkey" FOREIGN KEY ("workflow_config_id") REFERENCES "workflow_configs" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "workflows_workflow_config_id" to table: "workflows"
CREATE INDEX "workflows_workflow_config_id" ON "workflows" ("workflow_config_id");
-- Set comment to table: "workflows"
COMMENT ON TABLE "workflows" IS 'A named bucket of related agents. Pure organization; runs nothing.';
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
  PRIMARY KEY ("id"),
  CONSTRAINT "schedules_workflow_id_fkey" FOREIGN KEY ("workflow_id") REFERENCES "workflows" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "schedules_name_active" to table: "schedules"
CREATE UNIQUE INDEX "schedules_name_active" ON "schedules" ("name") WHERE (archived_at IS NULL);
-- Create index "schedules_workflow_id" to table: "schedules"
CREATE INDEX "schedules_workflow_id" ON "schedules" ("workflow_id");
-- Set comment to table: "schedules"
COMMENT ON TABLE "schedules" IS 'A cron-driven agent launch: when to fire and what to ask for.';
-- Create "agents" table
CREATE TABLE "agents" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "name" text NOT NULL,
  "cwd" text NOT NULL DEFAULT '',
  "flags" text NOT NULL DEFAULT '',
  "prompt" text NOT NULL DEFAULT '',
  "pane_id" text NOT NULL DEFAULT '',
  "state" text NOT NULL DEFAULT 'idle',
  "harness" text NOT NULL DEFAULT 'claude',
  "session_id" text NOT NULL DEFAULT '',
  "workflow_id" uuid NOT NULL,
  "schedule_id" uuid NULL,
  "idempotency_key" text NULL,
  "archived_at" timestamptz NULL,
  "is_hidden" boolean NOT NULL DEFAULT false,
  "mark" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "agents_schedule_id_fkey" FOREIGN KEY ("schedule_id") REFERENCES "schedules" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "agents_workflow_id_fkey" FOREIGN KEY ("workflow_id") REFERENCES "workflows" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "agents_idempotency_key" to table: "agents"
CREATE UNIQUE INDEX "agents_idempotency_key" ON "agents" ("idempotency_key") WHERE (idempotency_key IS NOT NULL);
-- Create index "agents_name_active" to table: "agents"
CREATE UNIQUE INDEX "agents_name_active" ON "agents" ("workflow_id", "name") WHERE (archived_at IS NULL);
-- Create index "agents_schedule_id" to table: "agents"
CREATE INDEX "agents_schedule_id" ON "agents" ("schedule_id");
-- Create index "agents_workflow_id" to table: "agents"
CREATE INDEX "agents_workflow_id" ON "agents" ("workflow_id");
-- Set comment to table: "agents"
COMMENT ON TABLE "agents" IS 'One background Claude agent, everything needed to restore it.';
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
COMMENT ON TABLE "agent_metadata" IS 'Key/value facts an agent recorded about its own work.';
-- Create "agent_transcript_chunks" table
CREATE TABLE "agent_transcript_chunks" (
  "agent_id" uuid NOT NULL,
  "seq" integer NOT NULL,
  "role" text NOT NULL,
  "content" text NOT NULL,
  "content_offset" bigint NOT NULL,
  "tsv" tsvector NOT NULL GENERATED ALWAYS AS (to_tsvector('english'::regconfig, content)) STORED,
  PRIMARY KEY ("agent_id", "seq"),
  CONSTRAINT "agent_transcript_chunks_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "agent_transcript_chunks_content_trgm" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_content_trgm" ON "agent_transcript_chunks" USING GIN ("content" gin_trgm_ops);
-- Create index "agent_transcript_chunks_tsv" to table: "agent_transcript_chunks"
CREATE INDEX "agent_transcript_chunks_tsv" ON "agent_transcript_chunks" USING GIN ("tsv");
-- Set comment to table: "agent_transcript_chunks"
COMMENT ON TABLE "agent_transcript_chunks" IS 'One turn of an agent''s transcript, word-indexed for search.';
-- Create "agent_transcripts" table
CREATE TABLE "agent_transcripts" (
  "agent_id" uuid NOT NULL,
  "content" text NOT NULL DEFAULT '',
  "synced_offset" bigint NOT NULL DEFAULT 0,
  "synced_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("agent_id"),
  CONSTRAINT "agent_transcripts_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "agent_transcripts"
COMMENT ON TABLE "agent_transcripts" IS 'Flattened transcript for an agent, synced from Claude''s on-disk JSONL and indexed for search. Strict 1:1 with an agent.';
