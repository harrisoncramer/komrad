-- name: InsertMail :one
INSERT INTO mail(
    from_agent_id,
    from_name,
    to_agent_id,
    body,
    kind,
    reply_to,
    about_agent_id,
    idempotency_key,
    expires_at)
VALUES (
    sqlc.narg('from_agent_id'),
    @from_name,
    @to_agent_id,
    @body,
    @kind,
    sqlc.narg('reply_to'),
    sqlc.narg('about_agent_id'),
    sqlc.narg('idempotency_key'),
    NOW() + interval '1 microsecond' * @deadline_micros::bigint)
RETURNING
    *;

-- name: GetMailByIdempotencyKey :one
SELECT
    *
FROM
    mail m
WHERE
    m.idempotency_key = @idempotency_key
LIMIT 1;

-- name: GetPendingMailOfKind :one
SELECT
    *
FROM
    mail m
WHERE
    m.to_agent_id = @to_agent_id
    AND m.kind = @kind
    AND m.about_agent_id = @about_agent_id::uuid
    AND m.claimed_at IS NULL
    AND m.expires_at > NOW()
ORDER BY
    m.id DESC
LIMIT 1;

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

-- name: ListMailByID :many
SELECT
    *
FROM
    mail m
WHERE
    m.id = ANY (@ids::uuid[]);

-- name: ListWatchedMail :many
SELECT
    *
FROM
    mail m
WHERE
    m.claimed_at IS NULL
    AND (sqlc.narg('keys')::text[] IS NULL
        OR m.id = ANY (sqlc.narg('keys')::uuid[]))
ORDER BY
    m.id;

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

-- name: ListMailSentPage :many
SELECT
    *
FROM
    mail m
WHERE
    m.from_agent_id = @from_agent_id
    AND (sqlc.narg('cursor_id')::uuid IS NULL
        OR m.id < sqlc.narg('cursor_id'))
ORDER BY
    m.id DESC
LIMIT @lim;

-- name: CountMailExchangedBetweenAgents :one
SELECT
    COUNT(*)
FROM
    mail m
WHERE
    m.kind = 'plain'
    AND ((m.from_agent_id = @one::uuid
            AND m.to_agent_id = @other::uuid)
        OR (m.from_agent_id = @other::uuid
            AND m.to_agent_id = @one::uuid));
