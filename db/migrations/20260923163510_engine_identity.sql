-- Modify "branch_scans" table
ALTER TABLE "branch_scans" DROP CONSTRAINT "branch_scans_pkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD PRIMARY KEY ("tenant_id", "repository_url", "branch");
-- Set comment to column: "tenant_id" on table: "branch_scans"
COMMENT ON COLUMN "branch_scans"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "forge_connections" table
ALTER TABLE "forge_connections" DROP CONSTRAINT "forge_connections_host_key", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "forge_connections_host_key" UNIQUE ("tenant_id", "host");
-- Set comment to column: "tenant_id" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "pull_request_syncs" table
ALTER TABLE "pull_request_syncs" DROP CONSTRAINT "pull_request_syncs_pkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD PRIMARY KEY ("tenant_id", "repository_url");
-- Set comment to column: "tenant_id" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "workflow_provisioner_configs" table
ALTER TABLE "workflow_provisioner_configs" ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "workflow_provisioner_configs_tenant_id_id_key" UNIQUE ("tenant_id", "id");
-- Set comment to column: "tenant_id" on table: "workflow_provisioner_configs"
COMMENT ON COLUMN "workflow_provisioner_configs"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "worktree_sizes" table
ALTER TABLE "worktree_sizes" DROP CONSTRAINT "worktree_sizes_pkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD PRIMARY KEY ("tenant_id", "dir");
-- Set comment to column: "tenant_id" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "workflow_configs" table
ALTER TABLE "workflow_configs" ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "workflow_configs_tenant_id_id_key" UNIQUE ("tenant_id", "id");
-- Set comment to column: "tenant_id" on table: "workflow_configs"
COMMENT ON COLUMN "workflow_configs"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "workflows" table
ALTER TABLE "workflows" DROP CONSTRAINT "workflows_provisioner_config_id_fkey", DROP CONSTRAINT "workflows_workflow_config_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "workflows_tenant_id_id_key" UNIQUE ("tenant_id", "id"), ADD CONSTRAINT "workflows_provisioner_config_id_fkey" FOREIGN KEY ("tenant_id", "provisioner_config_id") REFERENCES "workflow_provisioner_configs" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT, ADD CONSTRAINT "workflows_workflow_config_id_fkey" FOREIGN KEY ("tenant_id", "workflow_config_id") REFERENCES "workflow_configs" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Set comment to column: "tenant_id" on table: "workflows"
COMMENT ON COLUMN "workflows"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Drop index "schedules_name_active" from table: "schedules"
DROP INDEX "schedules_name_active";
-- Modify "schedules" table
ALTER TABLE "schedules" DROP CONSTRAINT "schedules_workflow_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "schedules_tenant_id_id_key" UNIQUE ("tenant_id", "id"), ADD CONSTRAINT "schedules_workflow_id_fkey" FOREIGN KEY ("tenant_id", "workflow_id") REFERENCES "workflows" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Create index "schedules_name_active" to table: "schedules"
CREATE UNIQUE INDEX "schedules_name_active" ON "schedules" ("tenant_id", "name") WHERE (archived_at IS NULL);
-- Set comment to column: "tenant_id" on table: "schedules"
COMMENT ON COLUMN "schedules"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Drop index "agents_idempotency_key" from table: "agents"
DROP INDEX "agents_idempotency_key";
-- Modify "agents" table
ALTER TABLE "agents" DROP CONSTRAINT "agents_parent_agent_id_fkey", DROP CONSTRAINT "agents_schedule_id_fkey", DROP CONSTRAINT "agents_spawned_by_agent_id_fkey", DROP CONSTRAINT "agents_workflow_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "agents_tenant_id_id_key" UNIQUE ("tenant_id", "id"), ADD CONSTRAINT "agents_parent_agent_id_fkey" FOREIGN KEY ("tenant_id", "parent_agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "agents_schedule_id_fkey" FOREIGN KEY ("tenant_id", "schedule_id") REFERENCES "schedules" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT, ADD CONSTRAINT "agents_spawned_by_agent_id_fkey" FOREIGN KEY ("tenant_id", "spawned_by_agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("spawned_by_agent_id"), ADD CONSTRAINT "agents_workflow_id_fkey" FOREIGN KEY ("tenant_id", "workflow_id") REFERENCES "workflows" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Create index "agents_idempotency_key" to table: "agents"
CREATE UNIQUE INDEX "agents_idempotency_key" ON "agents" ("tenant_id", "idempotency_key") WHERE (idempotency_key IS NOT NULL);
-- Set comment to column: "tenant_id" on table: "agents"
COMMENT ON COLUMN "agents"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "agent_metadata" table
ALTER TABLE "agent_metadata" DROP CONSTRAINT "agent_metadata_agent_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "agent_metadata_agent_id_fkey" FOREIGN KEY ("tenant_id", "agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "agent_metadata"
COMMENT ON COLUMN "agent_metadata"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "pull_requests" table
ALTER TABLE "pull_requests" DROP CONSTRAINT "pull_requests_host_provider_id_key", DROP CONSTRAINT "pull_requests_url_key", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "pull_requests_host_provider_id_key" UNIQUE ("tenant_id", "host", "provider_id"), ADD CONSTRAINT "pull_requests_url_key" UNIQUE ("tenant_id", "url"), ADD CONSTRAINT "pull_requests_tenant_id_id_key" UNIQUE ("tenant_id", "id");
-- Set comment to column: "tenant_id" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "agent_pull_requests" table
ALTER TABLE "agent_pull_requests" DROP CONSTRAINT "agent_pull_requests_agent_id_fkey", DROP CONSTRAINT "agent_pull_requests_pull_request_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "agent_pull_requests_agent_id_fkey" FOREIGN KEY ("tenant_id", "agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "agent_pull_requests_pull_request_id_fkey" FOREIGN KEY ("tenant_id", "pull_request_id") REFERENCES "pull_requests" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "agent_pull_requests"
COMMENT ON COLUMN "agent_pull_requests"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "agent_runs" table
ALTER TABLE "agent_runs" DROP CONSTRAINT "agent_runs_agent_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "agent_runs_agent_id_fkey" FOREIGN KEY ("tenant_id", "agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "agent_runs"
COMMENT ON COLUMN "agent_runs"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "agent_transcript_sessions" table
ALTER TABLE "agent_transcript_sessions" DROP CONSTRAINT "agent_transcript_sessions_agent_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "agent_transcript_sessions_tenant_id_id_key" UNIQUE ("tenant_id", "id"), ADD CONSTRAINT "agent_transcript_sessions_agent_id_fkey" FOREIGN KEY ("tenant_id", "agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "agent_transcript_sessions"
COMMENT ON COLUMN "agent_transcript_sessions"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "agent_transcript_chunks" table
ALTER TABLE "agent_transcript_chunks" DROP CONSTRAINT "agent_transcript_chunks_agent_id_fkey", DROP CONSTRAINT "agent_transcript_chunks_session_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "agent_transcript_chunks_agent_id_fkey" FOREIGN KEY ("tenant_id", "agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "agent_transcript_chunks_session_id_fkey" FOREIGN KEY ("tenant_id", "session_id") REFERENCES "agent_transcript_sessions" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "agent_transcript_chunks"
COMMENT ON COLUMN "agent_transcript_chunks"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "mail" table
ALTER TABLE "mail" DROP CONSTRAINT "mail_from_agent_id_fkey", DROP CONSTRAINT "mail_reply_to_fkey", DROP CONSTRAINT "mail_to_agent_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "mail_tenant_id_id_key" UNIQUE ("tenant_id", "id"), ADD CONSTRAINT "mail_from_agent_id_fkey" FOREIGN KEY ("tenant_id", "from_agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("from_agent_id"), ADD CONSTRAINT "mail_reply_to_fkey" FOREIGN KEY ("tenant_id", "reply_to") REFERENCES "mail" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("reply_to"), ADD CONSTRAINT "mail_to_agent_id_fkey" FOREIGN KEY ("tenant_id", "to_agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "mail"
COMMENT ON COLUMN "mail"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Drop index "marks_mark" from table: "marks"
DROP INDEX "marks_mark";
-- Modify "marks" table
ALTER TABLE "marks" DROP CONSTRAINT "marks_agent_id_fkey", DROP CONSTRAINT "marks_workflow_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "marks_agent_id_fkey" FOREIGN KEY ("tenant_id", "agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "marks_workflow_id_fkey" FOREIGN KEY ("tenant_id", "workflow_id") REFERENCES "workflows" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "marks_mark" to table: "marks"
CREATE UNIQUE INDEX "marks_mark" ON "marks" ("tenant_id", "mark");
-- Set comment to column: "tenant_id" on table: "marks"
COMMENT ON COLUMN "marks"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "review_rounds" table
ALTER TABLE "review_rounds" DROP CONSTRAINT "review_rounds_agent_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "review_rounds_tenant_id_id_key" UNIQUE ("tenant_id", "id"), ADD CONSTRAINT "review_rounds_agent_id_fkey" FOREIGN KEY ("tenant_id", "agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "reviews" table
ALTER TABLE "reviews" DROP CONSTRAINT "reviews_agent_id_fkey", DROP CONSTRAINT "reviews_author_agent_id_fkey", DROP CONSTRAINT "reviews_doorbell_mail_id_fkey", DROP CONSTRAINT "reviews_round_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "reviews_tenant_id_id_key" UNIQUE ("tenant_id", "id"), ADD CONSTRAINT "reviews_agent_id_fkey" FOREIGN KEY ("tenant_id", "agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "reviews_author_agent_id_fkey" FOREIGN KEY ("tenant_id", "author_agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("author_agent_id"), ADD CONSTRAINT "reviews_doorbell_mail_id_fkey" FOREIGN KEY ("tenant_id", "doorbell_mail_id") REFERENCES "mail" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("doorbell_mail_id"), ADD CONSTRAINT "reviews_round_id_fkey" FOREIGN KEY ("tenant_id", "round_id") REFERENCES "review_rounds" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "review_threads" table
ALTER TABLE "review_threads" DROP CONSTRAINT "review_threads_agent_id_fkey", DROP CONSTRAINT "review_threads_resolved_by_agent_id_fkey", DROP CONSTRAINT "review_threads_review_id_fkey", DROP CONSTRAINT "review_threads_round_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "review_threads_tenant_id_id_key" UNIQUE ("tenant_id", "id"), ADD CONSTRAINT "review_threads_agent_id_fkey" FOREIGN KEY ("tenant_id", "agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "review_threads_resolved_by_agent_id_fkey" FOREIGN KEY ("tenant_id", "resolved_by_agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("resolved_by_agent_id"), ADD CONSTRAINT "review_threads_review_id_fkey" FOREIGN KEY ("tenant_id", "review_id") REFERENCES "reviews" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "review_threads_round_id_fkey" FOREIGN KEY ("tenant_id", "round_id") REFERENCES "review_rounds" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "review_messages" table
ALTER TABLE "review_messages" DROP CONSTRAINT "review_messages_author_agent_id_fkey", DROP CONSTRAINT "review_messages_review_id_fkey", DROP CONSTRAINT "review_messages_thread_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "review_messages_author_agent_id_fkey" FOREIGN KEY ("tenant_id", "author_agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("author_agent_id"), ADD CONSTRAINT "review_messages_review_id_fkey" FOREIGN KEY ("tenant_id", "review_id") REFERENCES "reviews" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("review_id"), ADD CONSTRAINT "review_messages_thread_id_fkey" FOREIGN KEY ("tenant_id", "thread_id") REFERENCES "review_threads" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Modify "review_viewed_files" table
ALTER TABLE "review_viewed_files" DROP CONSTRAINT "review_viewed_files_agent_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "review_viewed_files_agent_id_fkey" FOREIGN KEY ("tenant_id", "agent_id") REFERENCES "agents" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "review_viewed_files"
COMMENT ON COLUMN "review_viewed_files"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
-- Set comment to table: "tenants"
COMMENT ON TABLE "tenants" IS 'An engine, the owner of every row it writes.';
-- Create "tenant_logins" table
CREATE TABLE "tenant_logins" (
  "role" text NOT NULL,
  "tenant_id" uuid NOT NULL,
  "reader" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("role"),
  CONSTRAINT "tenant_logins_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE FUNCTION current_tenant()
    RETURNS uuid
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, public
    AS $$
    SELECT
        tenant_id
    FROM
        public.tenant_logins
    WHERE
        ROLE = SESSION_USER;
$$;

-- Create index "tenant_logins_tenant_id" to table: "tenant_logins"
CREATE INDEX "tenant_logins_tenant_id" ON "tenant_logins" ("tenant_id");
-- Set comment to table: "tenant_logins"
COMMENT ON TABLE "tenant_logins" IS 'The Postgres logins an engine connects as, which is the only way an engine''s identity is known.';
-- Set comment to column: "reader" on table: "tenant_logins"
COMMENT ON COLUMN "tenant_logins"."reader" IS 'Whether this is the engine''s read-only login rather than the one it writes with.';
-- Modify "workflow_hooks" table
ALTER TABLE "workflow_hooks" DROP CONSTRAINT "workflow_hooks_workflow_id_fkey", ALTER COLUMN "tenant_id" DROP DEFAULT, ADD CONSTRAINT "workflow_hooks_workflow_id_fkey" FOREIGN KEY ("tenant_id", "workflow_id") REFERENCES "workflows" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "tenant_id" on table: "workflow_hooks"
COMMENT ON COLUMN "workflow_hooks"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE FUNCTION current_login_is_reader()
    RETURNS boolean
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, public
    AS $$
    SELECT
        COALESCE((
            SELECT
                reader
            FROM public.tenant_logins
            WHERE
                ROLE = SESSION_USER), FALSE);
$$;

CREATE FUNCTION stamp_tenant()
    RETURNS TRIGGER
    AS $$
BEGIN
    NEW.tenant_id := COALESCE(current_tenant(), NEW.tenant_id);
    RETURN NEW;
END;
$$
LANGUAGE plpgsql;

CREATE FUNCTION watch_channel_of(tenant uuid)
    RETURNS text
    LANGUAGE sql
    IMMUTABLE
    AS $$
    SELECT
        'weave_watch_' || REPLACE(tenant::text, '-', '');
$$;

CREATE FUNCTION watch_channel()
    RETURNS text
    LANGUAGE sql
    STABLE
    AS $$
    SELECT
        watch_channel_of(current_tenant());
$$;

CREATE FUNCTION listen_to_watch_channel()
    RETURNS void
    AS $$
BEGIN
    IF current_tenant() IS NULL THEN
        RAISE EXCEPTION 'this login belongs to no engine';
    END IF;
    EXECUTE FORMAT('LISTEN %I', watch_channel());
END;
$$
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION watch_notify()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM pg_notify(watch_channel_of(OLD.tenant_id), TG_TABLE_NAME || ' ' || OLD.id::text);
    ELSE
        PERFORM pg_notify(watch_channel_of(NEW.tenant_id), TG_TABLE_NAME || ' ' || NEW.id::text);
    END IF;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION watch_subresource()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF TG_OP <> 'INSERT' AND (to_jsonb(OLD) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify(watch_channel_of(OLD.tenant_id), TG_ARGV[0] || ' ' || (to_jsonb(OLD) ->> TG_ARGV[1]));
    END IF;
    IF TG_OP <> 'DELETE' AND (to_jsonb(NEW) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify(watch_channel_of(NEW.tenant_id), TG_ARGV[0] || ' ' || (to_jsonb(NEW) ->> TG_ARGV[1]));
    END IF;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION watch_subresource_lookup()
    RETURNS TRIGGER
    AS $$
DECLARE
    resource_id text;
    narrowing text := 'true';
    row_tenant uuid := (to_jsonb(COALESCE(NEW, OLD)) ->> 'tenant_id')::uuid;
BEGIN
    IF TG_ARGV[3] IS NOT NULL AND TG_ARGV[3] <> '' THEN
        narrowing := FORMAT('%I IS NULL', TG_ARGV[3]);
    END IF;
    FOR resource_id IN EXECUTE FORMAT('SELECT id::text FROM %I WHERE tenant_id = $2 AND %I::text = $1 AND (%s)', TG_ARGV[0], TG_ARGV[1], narrowing)
        USING (to_jsonb(COALESCE(NEW, OLD)) ->> TG_ARGV[2]), row_tenant LOOP
        PERFORM pg_notify(watch_channel_of(row_tenant), TG_ARGV[0] || ' ' || resource_id);
    END LOOP;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

DO $$
BEGIN
    BEGIN
        CREATE ROLE weave_engine NOLOGIN;
    EXCEPTION
        WHEN duplicate_object OR unique_violation THEN
            NULL;
    END;
    BEGIN
        CREATE ROLE weave_reader NOLOGIN;
    EXCEPTION
        WHEN duplicate_object OR unique_violation THEN
            NULL;
    END;
END;
$$;

DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY['weave_engine', 'weave_reader']
    LOOP
        IF EXISTS (
            SELECT
                1
            FROM
                pg_roles
            WHERE
                rolname = target
                AND (rolcanlogin
                    OR rolsuper
                    OR rolcreatedb
                    OR rolcreaterole
                    OR rolreplication
                    OR rolbypassrls)) THEN
        EXECUTE FORMAT('ALTER ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS', target);
    END IF;
        IF EXISTS (
            SELECT
                1
            FROM
                pg_auth_members
            WHERE
                member = target::regrole) THEN
        RAISE EXCEPTION '% is a member of another role, which every engine login would inherit', target;
    END IF;
END LOOP;
END;
$$;

GRANT USAGE ON SCHEMA public TO weave_engine, weave_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO weave_engine;

GRANT SELECT ON ALL TABLES IN SCHEMA public TO weave_reader;

REVOKE ALL ON tenant_logins FROM weave_engine, weave_reader;

REVOKE INSERT, UPDATE, DELETE ON tenants FROM weave_engine;

DO $$
BEGIN
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        REVOKE INSERT, UPDATE, DELETE ON schema_migrations FROM weave_engine;
    END IF;
END;
$$;

ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO weave_engine;

ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO weave_reader;

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;

CREATE POLICY engine_isolation ON tenants
    USING (id = (
            SELECT
                current_tenant()));

DO $$
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
        AND a.attname = 'tenant_id'
        AND NOT a.attisdropped
        AND c.relname <> 'tenant_logins' LOOP
            EXECUTE FORMAT('CREATE TRIGGER stamp_tenant BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION stamp_tenant()', target);
            EXECUTE FORMAT('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
            EXECUTE FORMAT('CREATE POLICY engine_isolation ON %I USING (tenant_id = (SELECT current_tenant())) WITH CHECK (tenant_id = (SELECT current_tenant()))', target);
        END LOOP;
END;
$$;
