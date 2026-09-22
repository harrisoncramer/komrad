DROP TRIGGER IF EXISTS watch_notify ON agents;
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON agents FOR EACH ROW EXECUTE FUNCTION watch_notify('id');

DROP TRIGGER IF EXISTS watch_project_agents ON agent_metadata;
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON agent_metadata FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');

DROP TRIGGER IF EXISTS watch_project_agents ON agent_runs;
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON agent_runs FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');

DROP TRIGGER IF EXISTS watch_project_agents ON reviews;
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON reviews FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');

DROP TRIGGER IF EXISTS watch_project_agents ON review_rounds;
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON review_rounds FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');

DROP TRIGGER IF EXISTS watch_project_agents ON pull_request_syncs;
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON pull_request_syncs FOR EACH ROW EXECUTE FUNCTION watch_project_lookup('agents', 'id', 'repository_url', 'repository_url', 'archived_at');

DROP TRIGGER IF EXISTS watch_notify ON workflows;
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON workflows FOR EACH ROW EXECUTE FUNCTION watch_notify('id');

DROP TRIGGER IF EXISTS watch_project_workflows ON workflow_hooks;
CREATE TRIGGER watch_project_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_hooks FOR EACH ROW EXECUTE FUNCTION watch_project('workflows', 'workflow_id');

DROP TRIGGER IF EXISTS watch_project_workflows ON workflow_configs;
CREATE TRIGGER watch_project_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_configs FOR EACH ROW EXECUTE FUNCTION watch_project_lookup('workflows', 'id', 'workflow_config_id', 'id', '');

DROP TRIGGER IF EXISTS watch_project_workflows ON workflow_provisioner_configs;
CREATE TRIGGER watch_project_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_provisioner_configs FOR EACH ROW EXECUTE FUNCTION watch_project_lookup('workflows', 'id', 'provisioner_config_id', 'id', '');

DROP TRIGGER IF EXISTS watch_notify ON schedules;
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON schedules FOR EACH ROW EXECUTE FUNCTION watch_notify('id');

DROP TRIGGER IF EXISTS watch_notify ON marks;
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON marks FOR EACH ROW EXECUTE FUNCTION watch_notify('mark');

DROP TRIGGER IF EXISTS watch_notify ON mail;
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON mail FOR EACH ROW EXECUTE FUNCTION watch_notify('id');

DROP TRIGGER IF EXISTS watch_notify ON pull_requests;
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON pull_requests FOR EACH ROW EXECUTE FUNCTION watch_notify('id');

DROP FUNCTION IF EXISTS attach_watch_trigger(text, text);
DROP FUNCTION IF EXISTS attach_projection_trigger(text, text, text);
DROP FUNCTION IF EXISTS attach_projection_lookup_trigger(text, text, text, text, text, text);
DROP FUNCTION IF EXISTS watch_trigger_is_current(text, text, text, text[]);
