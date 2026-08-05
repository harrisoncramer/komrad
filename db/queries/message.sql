-- name: InsertAgentMessage :one
INSERT INTO agent_messages(
    from_agent_id,
    from_name,
    to_agent_id,
    body,
    reply_to,
    hops,
    expires_at)
VALUES (
    sqlc.narg('from_agent_id'),
    @from_name,
    @to_agent_id,
    @body,
    sqlc.narg('reply_to'),
    @hops,
    @expires_at)
RETURNING
    *;

-- name: GetAgentMessage :one
SELECT
    *
FROM
    agent_messages m
WHERE
    m.id = @id;

-- name: ClaimAgentMessages :many
WITH pending AS (
    SELECT
        m.id
    FROM
        agent_messages m
    WHERE
        m.to_agent_id = @to_agent_id
        AND m.claimed_at IS NULL
        AND m.expires_at > NOW()
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
    AND m.claimed_at IS NULL
    AND m.expires_at > NOW();

-- name: ListExpiredAgentMessages :many
SELECT
    *
FROM
    agent_messages m
WHERE
    m.to_agent_id = @to_agent_id
    AND m.claimed_at IS NULL
    AND m.expires_at <= NOW()
ORDER BY
    m.id;

-- name: ListMailboxesWithPendingMail :many
SELECT DISTINCT
    m.to_agent_id
FROM
    agent_messages m
WHERE
    m.claimed_at IS NULL
    AND m.expires_at > NOW();
