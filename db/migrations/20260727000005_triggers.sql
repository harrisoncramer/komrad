SELECT
    attach_updated_at_triggers();

CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON agents FOR EACH ROW EXECUTE FUNCTION watch_notify();
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON agent_metadata FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON agent_runs FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON reviews FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON review_rounds FOR EACH ROW EXECUTE FUNCTION watch_project('agents', 'agent_id');
CREATE TRIGGER watch_project_agents AFTER INSERT OR UPDATE OR DELETE ON pull_request_syncs FOR EACH ROW EXECUTE FUNCTION watch_project_lookup('agents', 'repository_url', 'repository_url', 'archived_at');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON workflows FOR EACH ROW EXECUTE FUNCTION watch_notify();
CREATE TRIGGER watch_project_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_hooks FOR EACH ROW EXECUTE FUNCTION watch_project('workflows', 'workflow_id');
CREATE TRIGGER watch_project_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_configs FOR EACH ROW EXECUTE FUNCTION watch_project_lookup('workflows', 'workflow_config_id', 'id', '');
CREATE TRIGGER watch_project_workflows AFTER INSERT OR UPDATE OR DELETE ON workflow_provisioner_configs FOR EACH ROW EXECUTE FUNCTION watch_project_lookup('workflows', 'provisioner_config_id', 'id', '');
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON schedules FOR EACH ROW EXECUTE FUNCTION watch_notify();
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON marks FOR EACH ROW EXECUTE FUNCTION watch_notify();
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON mail FOR EACH ROW EXECUTE FUNCTION watch_notify();
CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON pull_requests FOR EACH ROW EXECUTE FUNCTION watch_notify();
