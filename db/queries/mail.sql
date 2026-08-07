-- name: InsertMail :one
INSERT INTO mail(
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

-- name: GetMail :one
SELECT
    *
FROM
    mail m
WHERE
    m.id = @id;

-- name: ClaimMail :many
WITH pending AS (
    SELECT
        m.id
    FROM
        mail m
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
    mail m
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

-- name: CountPendingMail :one
SELECT
    COUNT(*)
FROM
    mail m
WHERE
    m.to_agent_id = @to_agent_id
    AND m.claimed_at IS NULL
    AND m.expires_at > NOW();

-- name: ListMailboxesWithUnreadMail :many
SELECT DISTINCT
    m.to_agent_id
FROM
    mail m
WHERE
    m.claimed_at IS NULL
    AND m.expires_at > NOW();

-- name: ReportExpiredMail :many
WITH unreported AS (
    SELECT
        m.id
    FROM
        mail m
    WHERE
        m.to_agent_id = @to_agent_id
        AND m.claimed_at IS NULL
        AND m.reported_at IS NULL
        AND m.expires_at <= NOW()
    ORDER BY
        m.id
    FOR UPDATE
        SKIP LOCKED)
UPDATE
    mail m
SET
    reported_at = NOW()
WHERE
    m.id IN (
        SELECT
            unreported.id
        FROM
            unreported)
    RETURNING
        *;

-- name: ListMailPage :many
SELECT
    *
FROM
    mail m
WHERE
    m.to_agent_id = @to_agent_id
    AND (sqlc.narg('cursor_id')::uuid IS NULL
        OR m.id < sqlc.narg('cursor_id'))
ORDER BY
    m.id DESC
LIMIT @lim;

-- name: GetMailByID :many
SELECT
    *
FROM
    mail m
WHERE
    m.id = ANY (@ids::uuid[]);

-- name: DeleteMailByID :exec
DELETE FROM mail
WHERE id = ANY (@ids::uuid[]);

-- name: ListMailboxesWithPendingMail :many
SELECT DISTINCT
    m.to_agent_id
FROM
    mail m
WHERE
    m.claimed_at IS NULL
    AND (m.expires_at > NOW()
        OR m.reported_at IS NULL);
