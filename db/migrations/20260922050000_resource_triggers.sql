SELECT
    attach_watch_trigger('marks', 'mark');

SELECT
    attach_watch_trigger('mail', 'id');

SELECT
    attach_watch_trigger('pull_requests', 'id');

SELECT
    attach_projection_lookup_trigger('pull_request_syncs', 'agents', 'id', 'repository_url', 'repository_url', 'archived_at IS NULL');
