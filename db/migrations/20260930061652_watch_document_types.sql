SET LOCAL lock_timeout = '5s';

CREATE OR REPLACE FUNCTION watch_notify()
    RETURNS TRIGGER
    AS $$
DECLARE
    ignored text[] := TG_ARGV[1:TG_NARGS - 1];
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM pg_notify(watch_channel_of(OLD.engine_id), TG_TABLE_NAME || ' ' || (to_jsonb(OLD) ->> TG_ARGV[0]));
        RETURN NULL;
    END IF;
    IF TG_OP = 'UPDATE' AND to_jsonb(OLD) - ignored = to_jsonb(NEW) - ignored THEN
        RETURN NULL;
    END IF;
    PERFORM pg_notify(watch_channel_of(NEW.engine_id), TG_TABLE_NAME || ' ' || (to_jsonb(NEW) ->> TG_ARGV[0]));
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION watch_subresource()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF TG_OP <> 'INSERT' AND (to_jsonb(OLD) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify(watch_channel_of(OLD.engine_id), TG_ARGV[0] || ' ' || (to_jsonb(OLD) ->> TG_ARGV[1]));
    END IF;
    IF TG_OP <> 'DELETE' AND (to_jsonb(NEW) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify(watch_channel_of(NEW.engine_id), TG_ARGV[0] || ' ' || (to_jsonb(NEW) ->> TG_ARGV[1]));
    END IF;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION watch_subresource_lookup()
    RETURNS TRIGGER
    AS $$
DECLARE
    resource_key text;
    narrowing text := 'true';
    row_engine uuid := (to_jsonb(COALESCE(NEW, OLD)) ->> 'engine_id')::uuid;
    parent_type regtype;
BEGIN
    IF TG_OP = 'UPDATE' AND TG_ARGV[5] <> '' AND (to_jsonb(OLD) -> TG_ARGV[5]) IS NOT DISTINCT FROM (to_jsonb(NEW) -> TG_ARGV[5]) THEN
        RETURN NULL;
    END IF;
    IF TG_ARGV[4] <> '' THEN
        narrowing := FORMAT('%I IS NULL', TG_ARGV[4]);
    END IF;
    SELECT
        a.atttypid::regtype INTO parent_type
    FROM
        pg_attribute a
    WHERE
        a.attrelid = TG_ARGV[0]::regclass
        AND a.attname = TG_ARGV[2];
    FOR resource_key IN EXECUTE FORMAT('SELECT %I::text FROM %I WHERE engine_id = $2 AND %I = CAST($1 AS %s) AND (%s)', TG_ARGV[1], TG_ARGV[0], TG_ARGV[2], parent_type, narrowing)
        USING (to_jsonb(COALESCE(NEW, OLD)) ->> TG_ARGV[3]), row_engine LOOP
        PERFORM pg_notify(watch_channel_of(row_engine), TG_ARGV[0] || ' ' || resource_key);
    END LOOP;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

DO $$
DECLARE
    stale record;
BEGIN
    FOR stale IN
    SELECT
        tgname,
        tgrelid::regclass AS target
    FROM
        pg_trigger
    WHERE
        NOT tgisinternal
        AND tgfoid IN ('watch_notify'::regproc, 'watch_subresource'::regproc, 'watch_subresource_lookup'::regproc)
        LOOP
            EXECUTE FORMAT('DROP TRIGGER %I ON %s', stale.tgname, stale.target);
        END LOOP;
END;
$$;

CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON agents FOR EACH ROW EXECUTE FUNCTION watch_notify('id', 'state_seen_at', 'updated_at');
CREATE TRIGGER watch_subresource_agents AFTER INSERT OR UPDATE OR DELETE ON agent_runs FOR EACH ROW EXECUTE FUNCTION watch_subresource('agents', 'agent_id');
CREATE TRIGGER watch_subresource_agents AFTER INSERT OR UPDATE OR DELETE ON documents FOR EACH ROW EXECUTE FUNCTION watch_subresource('agents', 'agent_id');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON workflows FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_subresource_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_hooks FOR EACH ROW EXECUTE FUNCTION watch_subresource('workflows', 'workflow_id');
CREATE TRIGGER watch_subresource_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_configs FOR EACH ROW EXECUTE FUNCTION watch_subresource_lookup('workflows', 'id', 'workflow_config_id', 'id', '', '');
CREATE TRIGGER watch_subresource_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_provisioner_configs FOR EACH ROW EXECUTE FUNCTION watch_subresource_lookup('workflows', 'id', 'provisioner_config_id', 'id', '', '');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON schedules FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_subresource_schedules AFTER INSERT OR UPDATE OR DELETE ON schedule_runs FOR EACH ROW EXECUTE FUNCTION watch_subresource('schedules', 'schedule_id');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON mail FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON pull_requests FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_subresource_pull_requests AFTER INSERT OR UPDATE OR DELETE ON agent_pull_requests FOR EACH ROW EXECUTE FUNCTION watch_subresource('pull_requests', 'pull_request_id');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON pull_request_syncs FOR EACH ROW EXECUTE FUNCTION watch_notify('repository_url', 'calls', 'listed_at', 'read_at', 'sweep_cursor', 'sweep_newest');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON forge_review_threads FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_subresource_forge_review_threads AFTER INSERT OR UPDATE OR DELETE ON forge_review_comments FOR EACH ROW EXECUTE FUNCTION watch_subresource('forge_review_threads', 'thread_id');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON reviews FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_subresource_reviews AFTER INSERT OR UPDATE OR DELETE ON mail FOR EACH ROW EXECUTE FUNCTION watch_subresource_lookup('reviews', 'id', 'doorbell_mail_id', 'id', '', 'claimed_at');
CREATE TRIGGER watch_subresource_reviews AFTER INSERT OR UPDATE OR DELETE ON review_rounds FOR EACH ROW EXECUTE FUNCTION watch_subresource_lookup('reviews', 'id', 'round_id', 'id', '', '');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON review_threads FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_subresource_review_threads AFTER INSERT OR UPDATE OR DELETE ON review_messages FOR EACH ROW EXECUTE FUNCTION watch_subresource('review_threads', 'thread_id');
CREATE TRIGGER watch_subresource_review_threads AFTER INSERT OR UPDATE OR DELETE ON agents FOR EACH ROW EXECUTE FUNCTION watch_subresource_lookup('review_threads', 'id', 'agent_id', 'id', '', 'archived_at');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON worktrees FOR EACH ROW EXECUTE FUNCTION watch_notify('dir');
CREATE TRIGGER watch_subresource_worktrees AFTER INSERT OR UPDATE OR DELETE ON agents FOR EACH ROW EXECUTE FUNCTION watch_subresource_lookup('worktrees', 'dir', 'name', 'id', '', 'deleting_at');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON document_types FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
