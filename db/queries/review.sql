-- name: InsertReviewThreadWithMessage :one
WITH opened AS (
    INSERT INTO review_threads(
        agent_id,
        file_path,
        old_start,
        old_end,
        new_start,
        new_end,
        quoted_lines,
        last_agent_activity_at)
    VALUES (
        @agent_id,
        @file_path,
        @old_start,
        @old_end,
        @new_start,
        @new_end,
        @quoted_lines,
        CASE WHEN @author_kind::review_author_kind = 'agent' THEN
            NOW()
        END)
RETURNING
    *
),
said AS (
    INSERT INTO review_messages(
        thread_id,
        author_kind,
        author_agent_id,
        body)
    SELECT
        o.id,
        @author_kind::review_author_kind,
        sqlc.narg('author_agent_id'),
        @body
    FROM
        opened o
    RETURNING
        *
)
SELECT
    o.*
FROM
    opened o;

-- name: ListReviewThreads :many
SELECT
    t.*,
(t.last_agent_activity_at IS NOT NULL
        AND (t.human_read_at IS NULL
            OR t.last_agent_activity_at > t.human_read_at)) AS unread_by_human
FROM
    review_threads t
WHERE
    t.agent_id = @agent_id
ORDER BY
    t.file_path,
    t.new_start,
    t.old_start,
    t.id;

-- name: GetReviewThread :one
SELECT
    *
FROM
    review_threads t
WHERE
    t.id = @id;

-- name: MarkReviewThreadRead :one
UPDATE
    review_threads t
SET
    human_read_at = COALESCE(t.last_agent_activity_at, t.human_read_at)
WHERE
    t.id = @id
RETURNING
    *;

-- name: SetReviewThreadResolved :one
UPDATE
    review_threads t
SET
    resolved_at = CASE WHEN @resolved::boolean THEN
        NOW()
    ELSE
        NULL
    END
WHERE
    t.id = @id
RETURNING
    *;

-- name: DeleteReviewThread :exec
DELETE FROM review_threads
WHERE id = @id;

-- name: InsertReviewMessage :one
WITH written AS (
    INSERT INTO review_messages(
        thread_id,
        author_kind,
        author_agent_id,
        body)
    VALUES (
        @thread_id,
        @author_kind,
        sqlc.narg('author_agent_id'),
        @body)
RETURNING
    *
),
touched AS (
    UPDATE
        review_threads t
    SET
        last_agent_activity_at = CASE WHEN w.author_kind = 'agent' THEN
            NOW()
        ELSE
            t.last_agent_activity_at
        END,
        resolved_at = NULL
    FROM
        written w
    WHERE
        t.id = w.thread_id
    RETURNING
        t.id
)
SELECT
    w.*
FROM
    written w;

-- name: UpdateReviewMessageBody :one
WITH written AS (
    UPDATE
        review_messages m
    SET
        body = @body
    WHERE
        m.id = @id
    RETURNING
        *
),
touched AS (
    UPDATE
        review_threads t
    SET
        last_agent_activity_at = CASE WHEN w.author_kind = 'agent' THEN
            NOW()
        ELSE
            t.last_agent_activity_at
        END,
        resolved_at = NULL
    FROM
        written w
    WHERE
        t.id = w.thread_id
    RETURNING
        t.id
)
SELECT
    w.*
FROM
    written w;

-- name: GetReviewMessage :one
SELECT
    *
FROM
    review_messages m
WHERE
    m.id = @id;

-- name: ListReviewMessagesByThreads :many
WITH said AS (
    SELECT
        m.id
    FROM
        review_messages m
    WHERE
        m.thread_id = ANY (@thread_ids::uuid[])
),
doorbell AS (
    SELECT DISTINCT ON (l.review_message_id)
        l.review_message_id,
        l.claimed_at,
        l.expires_at
    FROM
        mail l
    WHERE
        l.review_message_id IN (
            SELECT
                said.id
            FROM
                said)
        ORDER BY
            l.review_message_id,
            l.id DESC
)
    SELECT
        m.*,
        d.claimed_at AS delivered_at,
(d.review_message_id IS NULL
            OR (d.claimed_at IS NULL
                AND d.expires_at <= NOW())) AS doorbell_expired
    FROM
        review_messages m
    LEFT JOIN doorbell d ON d.review_message_id = m.id
WHERE
    m.id IN (
        SELECT
            said.id
        FROM
            said)
    ORDER BY
        m.thread_id,
        m.id;

-- name: ReleaseUnsentReviewMessages :many
WITH unsent AS (
    SELECT
        m.id
    FROM
        review_messages m
        JOIN review_threads t ON t.id = m.thread_id
    WHERE
        t.agent_id = @agent_id
        AND m.author_kind = 'human'
        AND m.sent_at IS NULL)
UPDATE
    review_messages m
SET
    sent_at = NOW()
WHERE
    m.id IN (
        SELECT
            unsent.id
        FROM
            unsent)
RETURNING
    *;

-- name: ListExpiredReviewMessages :many
WITH released AS (
    SELECT
        m.id
    FROM
        review_messages m
        JOIN review_threads t ON t.id = m.thread_id
    WHERE
        t.agent_id = @agent_id
        AND m.author_kind = 'human'
        AND m.sent_at IS NOT NULL
),
doorbell AS (
    SELECT DISTINCT ON (l.review_message_id)
        l.review_message_id,
        l.claimed_at,
        l.expires_at
    FROM
        mail l
    WHERE
        l.review_message_id IN (
            SELECT
                released.id
            FROM
                released)
        ORDER BY
            l.review_message_id,
            l.id DESC
)
    SELECT
        m.*
    FROM
        review_messages m
    LEFT JOIN doorbell d ON d.review_message_id = m.id
WHERE
    m.id IN (
        SELECT
            released.id
        FROM
            released)
        AND (d.review_message_id IS NULL
            OR (d.claimed_at IS NULL
                AND d.expires_at <= NOW()))
    ORDER BY
        m.id;

-- name: DeleteReviewMessage :exec
DELETE FROM review_messages
WHERE id = @id;

-- name: MarkReviewFileViewed :one
INSERT INTO review_viewed_files(
    agent_id,
    file_path,
    fingerprint)
VALUES (
    @agent_id,
    @file_path,
    @fingerprint)
ON CONFLICT (
    agent_id,
    file_path)
    DO UPDATE SET
        fingerprint = EXCLUDED.fingerprint,
        viewed_at = NOW()
    RETURNING
        *;

-- name: UnmarkReviewFileViewed :exec
DELETE FROM review_viewed_files
WHERE agent_id = @agent_id
    AND file_path = @file_path;

-- name: ListReviewViewedFiles :many
SELECT
    v.*
FROM
    review_viewed_files v
WHERE
    v.agent_id = @agent_id
ORDER BY
    v.file_path;
