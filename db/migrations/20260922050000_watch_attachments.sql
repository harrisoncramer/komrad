-- Attaches the watch triggers to every table a resource is the rows of, and to every table
-- projected onto one.
--
-- Generated from the resources in internal/watch. Do not edit by hand; change that list and run
-- 'mise run gen:triggers'. The trigger functions this calls are hand-written, one migration
-- earlier.

SELECT
    attach_watch_trigger('agents', 'id');

SELECT
    attach_projection_trigger('agent_metadata', 'agents', 'agent_id');

SELECT
    attach_projection_trigger('agent_runs', 'agents', 'agent_id');

SELECT
    attach_projection_trigger('reviews', 'agents', 'agent_id');

SELECT
    attach_projection_trigger('review_rounds', 'agents', 'agent_id');

SELECT
    attach_projection_lookup_trigger('pull_request_syncs', 'agents', 'id', 'repository_url', 'repository_url', 'archived_at IS NULL');

SELECT
    attach_watch_trigger('workflows', 'id');

SELECT
    attach_projection_trigger('workflow_hooks', 'workflows', 'workflow_id');

SELECT
    attach_projection_lookup_trigger('workflow_configs', 'workflows', 'id', 'workflow_config_id', 'id', NULL);

SELECT
    attach_projection_lookup_trigger('workflow_provisioner_configs', 'workflows', 'id', 'provisioner_config_id', 'id', NULL);

SELECT
    attach_watch_trigger('schedules', 'id');

SELECT
    attach_watch_trigger('marks', 'mark');

SELECT
    attach_watch_trigger('mail', 'id');

SELECT
    attach_watch_trigger('pull_requests', 'id');
