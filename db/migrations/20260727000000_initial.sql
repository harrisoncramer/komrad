CREATE EXTENSION IF NOT EXISTS "pg_trgm";

CREATE TABLE "workflow_configs"(
    "id" uuid NOT NULL DEFAULT uuidv7(),
    "harness" text NOT NULL DEFAULT '',
    "cwd" text NOT NULL DEFAULT '',
    "flags" text NOT NULL DEFAULT '',
    "created_at" timestamptz NOT NULL DEFAULT NOW(),
    "updated_at" timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY ("id")
);

COMMENT ON TABLE "workflow_configs" IS 'Default config (harness, cwd, flags) seeded into new agents of a workflow.';

CREATE TABLE "workflows"(
    "id" uuid NOT NULL DEFAULT uuidv7(),
    "name" text NOT NULL,
    "context" text NOT NULL DEFAULT '',
    "setup_action" text NOT NULL DEFAULT '',
    "cleanup_action" text NOT NULL DEFAULT '',
    "workflow_config_id" uuid NOT NULL,
    "archived_at" timestamptz NULL,
    "created_at" timestamptz NOT NULL DEFAULT NOW(),
    "updated_at" timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY ("id"),
    CONSTRAINT "workflows_workflow_config_id_fkey" FOREIGN KEY ("workflow_config_id") REFERENCES "workflow_configs"("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);

CREATE INDEX "workflows_workflow_config_id" ON "workflows"("workflow_config_id");

COMMENT ON TABLE "workflows" IS 'A named bucket of related agents. Pure organization; runs nothing.';

CREATE TABLE "schedules"(
    "id" uuid NOT NULL DEFAULT uuidv7(),
    "name" text NOT NULL,
    "cron" text NOT NULL,
    "timezone" text NOT NULL DEFAULT '',
    "enabled" boolean NOT NULL DEFAULT TRUE,
    "hide_agents" boolean NOT NULL DEFAULT FALSE,
    "prompt" text NOT NULL DEFAULT '',
    "workflow_id" uuid NOT NULL,
    "archived_at" timestamptz NULL,
    "created_at" timestamptz NOT NULL DEFAULT NOW(),
    "updated_at" timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY ("id"),
    CONSTRAINT "schedules_workflow_id_fkey" FOREIGN KEY ("workflow_id") REFERENCES "workflows"("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);

CREATE UNIQUE INDEX "schedules_name_active" ON "schedules"("name")
WHERE (archived_at IS NULL);

CREATE INDEX "schedules_workflow_id" ON "schedules"("workflow_id");

COMMENT ON TABLE "schedules" IS 'A cron-driven agent launch: when to fire and what to ask for.';

CREATE TABLE "agents"(
    "id" uuid NOT NULL DEFAULT uuidv7(),
    "name" text NOT NULL,
    "cwd" text NOT NULL DEFAULT '',
    "flags" text NOT NULL DEFAULT '',
    "setup_action" text NULL,
    "cleanup_action" text NULL,
    "prompt" text NOT NULL DEFAULT '',
    "pane_id" text NOT NULL DEFAULT '',
    "state" text NOT NULL DEFAULT 'idle',
    "state_changed_at" timestamptz NOT NULL DEFAULT NOW(),
    "harness" text NOT NULL DEFAULT 'claude',
    "session_id" text NOT NULL DEFAULT '',
    "workflow_id" uuid NOT NULL,
    "schedule_id" uuid NULL,
    "idempotency_key" text NULL,
    "spawned_by_agent_id" uuid NULL,
    "archived_at" timestamptz NULL,
    "is_hidden" boolean NOT NULL DEFAULT FALSE,
    "nudged_at" timestamptz NULL,
    "mark" text NULL,
    "created_at" timestamptz NOT NULL DEFAULT NOW(),
    "updated_at" timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY ("id"),
    CONSTRAINT "agents_schedule_id_fkey" FOREIGN KEY ("schedule_id") REFERENCES "schedules"("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
    CONSTRAINT "agents_spawned_by_agent_id_fkey" FOREIGN KEY ("spawned_by_agent_id") REFERENCES "agents"("id") ON UPDATE NO ACTION ON DELETE SET NULL,
    CONSTRAINT "agents_workflow_id_fkey" FOREIGN KEY ("workflow_id") REFERENCES "workflows"("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);

CREATE UNIQUE INDEX "agents_idempotency_key" ON "agents"("idempotency_key")
WHERE (idempotency_key IS NOT NULL);

CREATE UNIQUE INDEX "agents_name_active" ON "agents"("workflow_id", "name")
WHERE (archived_at IS NULL);

CREATE INDEX "agents_schedule_id" ON "agents"("schedule_id");

CREATE INDEX "agents_workflow_id" ON "agents"("workflow_id");

COMMENT ON TABLE "agents" IS 'One background Claude agent, everything needed to restore it.';

CREATE TABLE "agent_metadata"(
    "agent_id" uuid NOT NULL,
    "key" text NOT NULL,
    "value" text NOT NULL,
    "created_at" timestamptz NOT NULL DEFAULT NOW(),
    "updated_at" timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY ("agent_id", "key"),
    CONSTRAINT "agent_metadata_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents"("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

COMMENT ON TABLE "agent_metadata" IS 'Key/value facts an agent recorded about its own work.';

CREATE TABLE "agent_transcript_chunks"(
    "agent_id" uuid NOT NULL,
    "seq" integer NOT NULL,
    "role" text NOT NULL,
    "content" text NOT NULL,
    "content_offset" bigint NOT NULL,
    "tsv" tsvector NOT NULL GENERATED ALWAYS AS (TO_TSVECTOR('english'::regconfig, content)) STORED,
    PRIMARY KEY ("agent_id", "seq"),
    CONSTRAINT "agent_transcript_chunks_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents"("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE INDEX "agent_transcript_chunks_content_trgm" ON "agent_transcript_chunks" USING GIN("content" gin_trgm_ops);

CREATE INDEX "agent_transcript_chunks_tsv" ON "agent_transcript_chunks" USING GIN("tsv");

COMMENT ON TABLE "agent_transcript_chunks" IS 'One turn of an agent''s transcript, word-indexed for search.';

CREATE TABLE "agent_transcripts"(
    "agent_id" uuid NOT NULL,
    "content" text NOT NULL DEFAULT '',
    "synced_offset" bigint NOT NULL DEFAULT 0,
    "synced_at" timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY ("agent_id"),
    CONSTRAINT "agent_transcripts_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents"("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

COMMENT ON TABLE "agent_transcripts" IS 'Flattened transcript for an agent, synced from Claude''s on-disk JSONL and indexed for search. Strict 1:1 with an agent.';

CREATE TABLE "mail"(
    "id" uuid NOT NULL DEFAULT uuidv7(),
    "from_agent_id" uuid NULL,
    "from_name" text NOT NULL DEFAULT '',
    "to_agent_id" uuid NOT NULL,
    "body" text NOT NULL,
    "reply_to" uuid NULL,
    "hops" integer NOT NULL DEFAULT 1,
    "claimed_at" timestamptz NULL,
    "reported_at" timestamptz NULL,
    "expires_at" timestamptz NOT NULL,
    "created_at" timestamptz NOT NULL DEFAULT NOW(),
    "updated_at" timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY ("id"),
    CONSTRAINT "mail_from_agent_id_fkey" FOREIGN KEY ("from_agent_id") REFERENCES "agents"("id") ON UPDATE NO ACTION ON DELETE SET NULL,
    CONSTRAINT "mail_reply_to_fkey" FOREIGN KEY ("reply_to") REFERENCES "mail"("id") ON UPDATE NO ACTION ON DELETE SET NULL,
    CONSTRAINT "mail_to_agent_id_fkey" FOREIGN KEY ("to_agent_id") REFERENCES "agents"("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE INDEX "mail_from_agent_id" ON "mail"("from_agent_id");

CREATE INDEX "mail_pending" ON "mail"("to_agent_id", "id")
WHERE (claimed_at IS NULL);

CREATE INDEX "mail_reply_to" ON "mail"("reply_to");

COMMENT ON TABLE "mail" IS 'One piece of mail from one agent to another, held until the recipient claims it.';

CREATE OR REPLACE FUNCTION set_updated_at()
    RETURNS TRIGGER
    AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION attach_updated_at_triggers()
    RETURNS void
    AS $$
DECLARE
    target text;
BEGIN
    FOR target IN
    SELECT
        c.relname
    FROM
        pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_attribute a ON a.attrelid = c.oid
    WHERE
        n.nspname = 'public'
        AND c.relkind = 'r'
        AND a.attname = 'updated_at'
        AND NOT a.attisdropped LOOP
            EXECUTE FORMAT('DROP TRIGGER IF EXISTS set_updated_at ON %I', target);
            EXECUTE FORMAT('CREATE TRIGGER set_updated_at BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION set_updated_at()', target);
        END LOOP;
END;
$$
LANGUAGE plpgsql;

SELECT
    attach_updated_at_triggers();
