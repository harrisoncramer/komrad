ALTER TABLE tenants RENAME TO engines;
ALTER TABLE tenant_logins RENAME TO engine_logins;
ALTER TABLE engine_logins RENAME COLUMN tenant_id TO engine_id;
ALTER INDEX tenant_logins_tenant_id RENAME TO engine_logins_engine_id;

ALTER TABLE agent_pull_requests RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE agent_runs RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE agent_transcript_chunks RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE agent_transcript_sessions RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE agents RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE branch_scans RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE forge_connections RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE mail RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE marks RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE pull_request_syncs RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE pull_requests RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE review_messages RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE review_rounds RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE review_threads RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE review_viewed_files RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE reviews RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE schedule_runs RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE schedules RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE workflow_configs RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE workflow_hooks RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE workflow_provisioner_configs RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE workflows RENAME COLUMN tenant_id TO engine_id;
ALTER TABLE worktrees RENAME COLUMN tenant_id TO engine_id;

DO $$
DECLARE
    stale record;
BEGIN
    FOR stale IN
    SELECT
        conrelid::regclass AS target,
        conname
    FROM
        pg_constraint
    WHERE
        connamespace = 'public'::regnamespace
        AND conname LIKE '%tenant%' LOOP
            EXECUTE FORMAT('ALTER TABLE %s RENAME CONSTRAINT %I TO %I', stale.target, stale.conname, REPLACE(stale.conname, 'tenant', 'engine'));
        END LOOP;
END;
$$;

DO $$
DECLARE
    target regclass;
BEGIN
    FOR target IN
    SELECT
        tgrelid::regclass
    FROM
        pg_trigger
    WHERE
        tgname = 'stamp_tenant' LOOP
            EXECUTE FORMAT('ALTER TRIGGER stamp_tenant ON %s RENAME TO stamp_engine', target);
        END LOOP;
END;
$$;

ALTER FUNCTION current_tenant() RENAME TO current_engine;

CREATE OR REPLACE FUNCTION current_engine()
    RETURNS uuid
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, public
    AS $$
    SELECT
        engine_id
    FROM
        public.engine_logins
    WHERE
        ROLE = SESSION_USER;
$$;

CREATE OR REPLACE FUNCTION current_login_is_reader()
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
            FROM public.engine_logins
            WHERE
                ROLE = SESSION_USER), FALSE);
$$;

ALTER FUNCTION stamp_tenant() RENAME TO stamp_engine;

CREATE OR REPLACE FUNCTION stamp_engine()
    RETURNS TRIGGER
    AS $$
BEGIN
    NEW.engine_id := COALESCE(current_engine(), NEW.engine_id);
    RETURN NEW;
END;
$$
LANGUAGE plpgsql;

DROP FUNCTION watch_channel_of(uuid);

CREATE FUNCTION watch_channel_of(engine uuid)
    RETURNS text
    LANGUAGE sql
    IMMUTABLE
    AS $$
    SELECT
        'komrad_watch_' || REPLACE(engine::text, '-', '');
$$;

CREATE OR REPLACE FUNCTION watch_channel()
    RETURNS text
    LANGUAGE sql
    STABLE
    AS $$
    SELECT
        watch_channel_of(current_engine());
$$;

CREATE OR REPLACE FUNCTION listen_to_watch_channel()
    RETURNS void
    AS $$
BEGIN
    IF current_engine() IS NULL THEN
        RAISE EXCEPTION 'this login belongs to no engine';
    END IF;
    EXECUTE FORMAT('LISTEN %I', watch_channel());
END;
$$
LANGUAGE plpgsql;

DO $$
DECLARE
    target regclass;
BEGIN
    FOR target IN
    SELECT
        polrelid::regclass
    FROM
        pg_policy
    WHERE
        polname = 'engine_isolation'
        AND polrelid <> 'engines'::regclass LOOP
            EXECUTE FORMAT('ALTER POLICY engine_isolation ON %s USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()))', target);
        END LOOP;
END;
$$;

ALTER POLICY engine_isolation ON engines
    USING (id = (
            SELECT
                current_engine()));
