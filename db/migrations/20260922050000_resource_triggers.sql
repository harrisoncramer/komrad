-- attach_projection_trigger puts watch_project on a table whose rows are rendered onto a parent
-- row, the parent's key held in parent_column, named after the parent so a table can feed more
-- than one.
CREATE OR REPLACE FUNCTION attach_projection_trigger(target text, parent text, parent_column text, ignored text[])
    RETURNS void
    AS $$
BEGIN
    EXECUTE FORMAT('DROP TRIGGER IF EXISTS watch_project ON %I', target);
    EXECUTE FORMAT('DROP TRIGGER IF EXISTS %I ON %I', 'watch_project_' || parent, target);
    EXECUTE FORMAT('CREATE TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION watch_project(%L, %L, %L)', 'watch_project_' || parent, target, parent, parent_column, ignored);
END;
$$
LANGUAGE plpgsql;

-- attach_projection_lookup_trigger puts watch_project_lookup on a table whose parents are the
-- rows of parent whose parent_match column equals this table's match column, narrowed by an
-- extra WHERE when one is given, named after the parent the same way.
CREATE OR REPLACE FUNCTION attach_projection_lookup_trigger(target text, parent text, parent_key text, parent_match text, match text, ignored text[], extra_where text)
    RETURNS void
    AS $$
BEGIN
    EXECUTE FORMAT('DROP TRIGGER IF EXISTS watch_project ON %I', target);
    EXECUTE FORMAT('DROP TRIGGER IF EXISTS %I ON %I', 'watch_project_' || parent, target);
    EXECUTE FORMAT('CREATE TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION watch_project_lookup(%L, %L, %L, %L, %L, %L)', 'watch_project_' || parent, target, parent, parent_key, parent_match, match, ignored, COALESCE(extra_where, 'true'));
END;
$$
LANGUAGE plpgsql;

SELECT
    attach_projection_trigger('agent_metadata', 'agents', 'agent_id', '{updated_at}');

SELECT
    attach_projection_trigger('agent_runs', 'agents', 'agent_id', '{}');

SELECT
    attach_projection_trigger('reviews', 'agents', 'agent_id', '{updated_at}');

SELECT
    attach_projection_trigger('review_rounds', 'agents', 'agent_id', '{}');

SELECT
    attach_projection_trigger('workflow_hooks', 'workflows', 'workflow_id', '{}');

SELECT
    attach_projection_lookup_trigger('workflow_configs', 'workflows', 'id', 'workflow_config_id', 'id', '{updated_at}', NULL);

SELECT
    attach_projection_lookup_trigger('workflow_provisioner_configs', 'workflows', 'id', 'provisioner_config_id', 'id', '{updated_at}', NULL);

SELECT
    attach_watch_trigger('marks', 'mark', '{}');

SELECT
    attach_watch_trigger('mail', 'id', '{updated_at,reported_at}');

SELECT
    attach_watch_trigger('pull_requests', 'id', '{checks_read_at}');

SELECT
    attach_projection_trigger('marks', 'agents', 'agent_id', '{}');

SELECT
    attach_projection_trigger('marks', 'workflows', 'workflow_id', '{}');

SELECT
    attach_projection_trigger('mail', 'agents', 'to_agent_id', '{updated_at,reported_at}');

SELECT
    attach_projection_lookup_trigger('pull_requests', 'agents', 'id', 'branch', 'head_branch', '{checks_read_at}', 'archived_at IS NULL');

SELECT
    attach_projection_lookup_trigger('pull_request_syncs', 'agents', 'id', 'repository_url', 'repository_url', '{read_at,calls,listed_at,sweep_cursor,sweep_newest}', 'archived_at IS NULL');
