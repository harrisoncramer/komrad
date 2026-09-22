SELECT
    attach_watch_trigger('marks', 'mark', '{}');

SELECT
    attach_watch_trigger('mail', 'id', '{updated_at}');

SELECT
    attach_watch_trigger('pull_requests', 'id', '{checks_read_at}');

SELECT
    attach_projection_lookup_trigger('pull_request_syncs', 'agents', 'id', 'repository_url', 'repository_url', '{read_at,calls,listed_at,sweep_cursor,sweep_newest}', 'archived_at IS NULL');
