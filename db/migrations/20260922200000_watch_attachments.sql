SET LOCAL lock_timeout = '5s';

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
        AND tgfoid IN ('watch_notify'::regproc, 'watch_project'::regproc, 'watch_project_lookup'::regproc)
        LOOP
            EXECUTE FORMAT('DROP TRIGGER %I ON %s', stale.tgname, stale.target);
        END LOOP;
END;
$$;

CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON agents FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON agent_metadata FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON agent_runs FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON reviews FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON review_rounds FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON pull_request_syncs FOR EACH ROW EXECUTE FUNCTION watch_project_lookup('agents', 'id', 'repository_url', 'repository_url', 'archived_at');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON workflows FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_project_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_hooks FOR EACH ROW EXECUTE FUNCTION watch_project('workflows', 'workflow_id');
CREATE TRIGGER watch_project_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_configs FOR EACH ROW EXECUTE FUNCTION watch_project_lookup('workflows', 'id', 'workflow_config_id', 'id', '');
CREATE TRIGGER watch_project_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_provisioner_configs FOR EACH ROW EXECUTE FUNCTION watch_project_lookup('workflows', 'id', 'provisioner_config_id', 'id', '');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON schedules FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON marks FOR EACH ROW EXECUTE FUNCTION watch_notify('mark');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON mail FOR EACH ROW EXECUTE FUNCTION watch_notify('id');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON pull_requests FOR EACH ROW EXECUTE FUNCTION watch_notify('id');

DROP FUNCTION IF EXISTS attach_watch_trigger(text, text);
DROP FUNCTION IF EXISTS attach_projection_trigger(text, text, text);
DROP FUNCTION IF EXISTS attach_projection_lookup_trigger(text, text, text, text, text, text);
DROP FUNCTION IF EXISTS watch_trigger_is_current(text, text, text, text[]);
