-- name: InsertAgentMessage :one
INSERT INTO agent_messages(
    from_agent_id,
    from_name,
    to_agent_id,
    body,
    reply_to)
VALUES (
    sqlc.narg('from_agent_id'),
    @from_name,
    @to_agent_id,
    @body,
    sqlc.narg('reply_to'))
RETURNING
    *;

-- name: ClaimAgentMessages :many
WITH pending AS (
    SELECT
        m.id
    FROM
        agent_messages m
    WHERE
        m.to_agent_id = @to_agent_id
        AND m.claimed_at IS NULL
    ORDER BY
        m.id
    LIMIT @lim
    FOR UPDATE
        SKIP LOCKED)
UPDATE
    agent_messages m
SET
    claimed_at = NOW()
WHERE
    m.id IN (
        SELECT
            pending.id
        FROM
            pending)
RETURNING
    *;

-- name: CountPendingAgentMessages :one
SELECT
    COUNT(*)
FROM
    agent_messages m
WHERE
    m.to_agent_id = @to_agent_id
    AND m.claimed_at IS NULL;
