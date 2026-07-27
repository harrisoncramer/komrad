-- pg_trgm backs the transcript search index below.
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

-- Create "agent_configs" table
CREATE TABLE "agent_configs" (
    "id" uuid NOT NULL DEFAULT uuidv7 (),
    "cwd" text NOT NULL DEFAULT '',
    "flags" text NOT NULL DEFAULT '',
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "updated_at" timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY ("id")
);

-- Set comment to table: "agent_configs"
COMMENT ON TABLE "agent_configs" IS 'Editable config (cwd, flags) owned by exactly one agent.';

-- Create "agent_transcripts" table
CREATE TABLE "agent_transcripts" (
    "agent_id" uuid NOT NULL,
    "content" text NOT NULL DEFAULT '',
    "synced_offset" bigint NOT NULL DEFAULT 0,
    "synced_at" timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY ("agent_id")
);

-- Create index "agent_transcripts_content_trgm" to table: "agent_transcripts"
CREATE INDEX "agent_transcripts_content_trgm" ON "agent_transcripts" USING GIN ("content" gin_trgm_ops);

-- Set comment to table: "agent_transcripts"
COMMENT ON TABLE "agent_transcripts" IS 'Flattened transcript for an agent, synced from Claude''s on-disk JSONL and indexed for search. Strict 1:1 with an agent.';

-- Create "agents" table
CREATE TABLE "agents" (
    "id" uuid NOT NULL DEFAULT uuidv7 (),
    "name" text NOT NULL,
    "agent_config_id" uuid NOT NULL,
    "prompt" text NOT NULL DEFAULT '',
    "pane_id" text NOT NULL DEFAULT '',
    "state" text NOT NULL DEFAULT 'new',
    "claude_session_id" text NOT NULL DEFAULT '',
    "workflow_id" uuid NULL,
    "is_deleted" boolean NOT NULL DEFAULT FALSE,
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "updated_at" timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY ("id")
);

-- Create index "agents_name_active" to table: "agents"
CREATE UNIQUE INDEX "agents_name_active" ON "agents" ("name")
WHERE (is_deleted = FALSE);

-- Create index "agents_updated_at_id" to table: "agents"
CREATE INDEX "agents_updated_at_id" ON "agents" ("updated_at" DESC, "id" DESC);

-- Create index "agents_workflow_id" to table: "agents"
CREATE INDEX "agents_workflow_id" ON "agents" ("workflow_id");

-- Set comment to table: "agents"
COMMENT ON TABLE "agents" IS 'One background Claude agent, everything needed to restore it.';

-- Create "workflow_configs" table
CREATE TABLE "workflow_configs" (
    "id" uuid NOT NULL DEFAULT uuidv7 (),
    "cwd" text NOT NULL DEFAULT '',
    "flags" text NOT NULL DEFAULT '',
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "updated_at" timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY ("id")
);

-- Set comment to table: "workflow_configs"
COMMENT ON TABLE "workflow_configs" IS 'Default config (cwd, flags) seeded into new agents of a workflow.';

-- Create "workflows" table
CREATE TABLE "workflows" (
    "id" uuid NOT NULL DEFAULT uuidv7 (),
    "name" text NOT NULL,
    "context" text NOT NULL DEFAULT '',
    "workflow_config_id" uuid NOT NULL,
    "is_deleted" boolean NOT NULL DEFAULT FALSE,
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "updated_at" timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY ("id")
);

-- Set comment to table: "workflows"
COMMENT ON TABLE "workflows" IS 'A named bucket of related agents. Pure organization; runs nothing.';

-- Maintain updated_at in the database rather than in each statement, so a row's
-- timestamp is correct no matter which query wrote it, including the statements
-- the store issues directly rather than through sqlc.
CREATE OR REPLACE FUNCTION set_updated_at ()
    RETURNS TRIGGER
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER trg_agents_updated_at
    BEFORE UPDATE ON "agents"
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at ();

CREATE TRIGGER trg_agent_configs_updated_at
    BEFORE UPDATE ON "agent_configs"
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at ();

CREATE TRIGGER trg_workflows_updated_at
    BEFORE UPDATE ON "workflows"
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at ();

CREATE TRIGGER trg_workflow_configs_updated_at
    BEFORE UPDATE ON "workflow_configs"
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at ();

