-- Create "session_configs" table
CREATE TABLE "session_configs" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "cwd" text NOT NULL DEFAULT '',
  "flags" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Set comment to table: "session_configs"
COMMENT ON TABLE "session_configs" IS 'Editable config (cwd, flags) owned by exactly one session.';
-- Create "session_transcripts" table
CREATE TABLE "session_transcripts" (
  "session_id" uuid NOT NULL,
  "content" text NOT NULL DEFAULT '',
  "synced_offset" bigint NOT NULL DEFAULT 0,
  "synced_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("session_id")
);
-- Create index "session_transcripts_content_trgm" to table: "session_transcripts"
CREATE INDEX "session_transcripts_content_trgm" ON "session_transcripts" USING GIN ("content" gin_trgm_ops);
-- Set comment to table: "session_transcripts"
COMMENT ON TABLE "session_transcripts" IS 'Flattened chat transcript for a session, synced from Claude''s on-disk JSONL and indexed for search. Strict 1:1 with a session.';
-- Create "sessions" table
CREATE TABLE "sessions" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "name" text NOT NULL,
  "session_config_id" uuid NOT NULL,
  "prompt" text NOT NULL DEFAULT '',
  "pane_id" text NOT NULL DEFAULT '',
  "state" text NOT NULL DEFAULT 'new',
  "claude_session_id" text NOT NULL DEFAULT '',
  "workflow_id" uuid NULL,
  "is_deleted" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "sessions_name_active" to table: "sessions"
CREATE UNIQUE INDEX "sessions_name_active" ON "sessions" ("name") WHERE (is_deleted = false);
-- Create index "sessions_updated_at_id" to table: "sessions"
CREATE INDEX "sessions_updated_at_id" ON "sessions" ("updated_at" DESC, "id" DESC);
-- Create index "sessions_workflow_id" to table: "sessions"
CREATE INDEX "sessions_workflow_id" ON "sessions" ("workflow_id");
-- Set comment to table: "sessions"
COMMENT ON TABLE "sessions" IS 'One background Claude agent, everything needed to restore it.';
-- Create "workflow_configs" table
CREATE TABLE "workflow_configs" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "cwd" text NOT NULL DEFAULT '',
  "flags" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Set comment to table: "workflow_configs"
COMMENT ON TABLE "workflow_configs" IS 'Default config (cwd, flags) seeded into new chats of a workflow.';
-- Create "workflows" table
CREATE TABLE "workflows" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "name" text NOT NULL,
  "context" text NOT NULL DEFAULT '',
  "workflow_config_id" uuid NOT NULL,
  "is_deleted" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Set comment to table: "workflows"
COMMENT ON TABLE "workflows" IS 'A named bucket of related chats. Pure organization; runs nothing.';
