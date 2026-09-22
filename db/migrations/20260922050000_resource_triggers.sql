SELECT
    attach_watch_trigger('marks', 'mark', '{}');

SELECT
    attach_watch_trigger('mail', 'id', '{updated_at}');

SELECT
    attach_watch_trigger('pull_requests', 'id', '{checks_read_at}');

SELECT
    attach_projection_trigger('marks', 'agents', 'agent_id', '{}');

SELECT
    attach_projection_trigger('marks', 'workflows', 'workflow_id', '{}');

SELECT
    attach_projection_trigger('mail', 'agents', 'to_agent_id', '{updated_at}');

SELECT
    attach_projection_lookup_trigger('pull_requests', 'agents', 'id', 'branch', 'head_branch', '{checks_read_at}', 'archived_at IS NULL');

SELECT
    attach_projection_lookup_trigger('pull_request_syncs', 'agents', 'id', 'repository_url', 'repository_url', '{read_at,calls,listed_at,sweep_cursor,sweep_newest}', 'archived_at IS NULL');
