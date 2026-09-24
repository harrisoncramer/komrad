-- name: ListAgentRuns :many
SELECT
    id::text AS id,
    TRIGGER,
    script,
    log_number,
    exit_code,
    failure
FROM
    agent_runs
WHERE
    agent_id::text = @agent_id::text
ORDER BY
    started_at,
    id;
