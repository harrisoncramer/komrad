-- name: InsertAgentRun :one
INSERT INTO agent_runs(
    agent_id,
    TRIGGER,
    script,
    log_number)
VALUES (
    @agent_id,
    @trigger,
    @script,
    sqlc.narg('log_number'))
RETURNING
    id;

-- name: FinishAgentRun :exec
UPDATE
    agent_runs
SET
    finished_at = NOW(),
    exit_code = sqlc.narg('exit_code'),
    failure = @failure
WHERE
    id = @id;

-- name: DeleteAgentRun :exec
DELETE FROM agent_runs
WHERE id = @id;

-- name: PruneAgentRuns :exec
WITH kept AS (
    SELECT
        id
    FROM
        agent_runs
    WHERE
        agent_id = @agent_id
    ORDER BY
        started_at DESC,
        id DESC
    LIMIT @keep)
DELETE FROM agent_runs
WHERE agent_runs.agent_id = @agent_id
    AND agent_runs.id NOT IN (
        SELECT
            id
        FROM
            kept);

-- name: MarkAgentRunsRead :exec
UPDATE
    agent_runs
SET
    viewed_at = NOW()
WHERE
    agent_id = @agent_id
    AND failure <> ''
    AND viewed_at IS NULL;
