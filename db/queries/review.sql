-- name: InsertReviewThreadWithMessage :one
WITH thread AS (
    INSERT INTO review_threads(
        agent_id,
        file_path,
        old_start,
        old_end,
        new_start,
        new_end,
        quoted_lines,
        last_human_activity_at,
        last_agent_activity_at)
    VALUES (
        @agent_id,
        @file_path,
        @old_start,
        @old_end,
        @new_start,
        @new_end,
        @quoted_lines,
        CASE WHEN @author_kind::review_author_kind = 'human' THEN
            NOW()
        END,
        CASE WHEN @author_kind::review_author_kind = 'agent' THEN
            NOW()
        END)
RETURNING
    *
),
message AS (
    INSERT INTO review_messages(
        thread_id,
        author_kind,
        author_agent_id,
        body)
    SELECT
        t.id,
        @author_kind,
        sqlc.narg('author_agent_id'),
        @body
    FROM
        thread t
    RETURNING
        *
)
SELECT
    t.id,
    t.agent_id,
    t.file_path,
    t.old_start,
    t.old_end,
    t.new_start,
    t.new_end,
    t.quoted_lines,
    t.resolved_at,
    t.last_human_activity_at,
    t.last_agent_activity_at,
    t.agent_read_at,
    t.human_read_at,
    t.created_at,
    t.updated_at,
    m.id AS message_id,
    m.author_kind AS message_author_kind,
    m.author_agent_id AS message_author_agent_id,
    m.body AS message_body,
    m.created_at AS message_created_at,
    m.updated_at AS message_updated_at
FROM
    thread t,
    message m;

-- name: ListReviewThreads :many
SELECT
    sqlc.embed(t),
(t.last_human_activity_at IS NOT NULL
        AND (t.agent_read_at IS NULL
            OR t.last_human_activity_at > t.agent_read_at)) AS unread_by_agent,
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

-- name: ClaimReviewThreadsForAgent :many
WITH BEFORE AS (
    SELECT
        t.id,
        t.agent_read_at,
        t.human_read_at,
        t.last_human_activity_at,
        t.last_agent_activity_at
    FROM
        review_threads t
    WHERE
        t.agent_id = @agent_id
    ORDER BY
        t.id
    FOR UPDATE
),
claimed AS (
    UPDATE
        review_threads t
    SET
        agent_read_at = before.last_human_activity_at
    FROM
        BEFORE
    WHERE
        t.id = before.id
        AND before.last_human_activity_at IS NOT NULL
    RETURNING
        t.id
)
SELECT
    sqlc.embed(t),
(before.last_human_activity_at IS NOT NULL
        AND (before.agent_read_at IS NULL
            OR before.last_human_activity_at > before.agent_read_at)) AS unread_by_agent,
(before.last_agent_activity_at IS NOT NULL
        AND (before.human_read_at IS NULL
            OR before.last_agent_activity_at > before.human_read_at)) AS unread_by_human
FROM
    review_threads t
    JOIN BEFORE ON before.id = t.id
ORDER BY
    t.file_path,
    t.new_start,
    t.old_start,
    t.id;

-- name: GetReviewThread :one
SELECT
    sqlc.embed(t),
(t.last_human_activity_at IS NOT NULL
        AND (t.agent_read_at IS NULL
            OR t.last_human_activity_at > t.agent_read_at)) AS unread_by_agent,
(t.last_agent_activity_at IS NOT NULL
        AND (t.human_read_at IS NULL
            OR t.last_agent_activity_at > t.human_read_at)) AS unread_by_human
FROM
    review_threads t
WHERE
    t.id = @id;

-- name: MarkReviewThreadReadByHuman :one
UPDATE
    review_threads t
SET
    human_read_at = t.last_agent_activity_at
WHERE
    t.id = @id
RETURNING
    *;

-- name: SetReviewThreadResolved :one
UPDATE
    review_threads
SET
    resolved_at = sqlc.narg('resolved_at')
WHERE
    id = @id
RETURNING
    *;

-- name: CountUnreadReviewThreads :one
SELECT
    COUNT(*)
FROM
    review_threads t
WHERE
    t.agent_id = @agent_id
    AND t.resolved_at IS NULL
    AND t.last_human_activity_at IS NOT NULL
    AND (t.agent_read_at IS NULL
        OR t.last_human_activity_at > t.agent_read_at);

-- name: ListAgentsWithUnreadReviewThreads :many
SELECT DISTINCT
    t.agent_id
FROM
    review_threads t
WHERE
    t.resolved_at IS NULL
    AND t.last_human_activity_at IS NOT NULL
    AND (t.agent_read_at IS NULL
        OR t.last_human_activity_at > t.agent_read_at);

-- name: DeleteReviewThread :exec
DELETE FROM review_threads
WHERE id = @id;

-- name: InsertReviewMessage :one
WITH inserted AS (
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
        last_human_activity_at = CASE WHEN inserted.author_kind = 'human' THEN
            inserted.created_at
        ELSE
            t.last_human_activity_at
        END,
        last_agent_activity_at = CASE WHEN inserted.author_kind = 'agent' THEN
            inserted.created_at
        ELSE
            t.last_agent_activity_at
        END,
        resolved_at = NULL
    FROM
        inserted
    WHERE
        t.id = inserted.thread_id
    RETURNING
        t.id
)
SELECT
    inserted.*
FROM
    inserted;

-- name: UpdateReviewMessageBody :one
WITH updated AS (
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
        last_human_activity_at = CASE WHEN updated.author_kind = 'human' THEN
            updated.updated_at
        ELSE
            t.last_human_activity_at
        END,
        last_agent_activity_at = CASE WHEN updated.author_kind = 'agent' THEN
            updated.updated_at
        ELSE
            t.last_agent_activity_at
        END,
        resolved_at = NULL
    FROM
        updated
    WHERE
        t.id = updated.thread_id
    RETURNING
        t.id
)
SELECT
    updated.*
FROM
    updated;

-- name: GetReviewMessage :one
SELECT
    *
FROM
    review_messages m
WHERE
    m.id = @id;

-- name: ListReviewMessagesByThreads :many
SELECT
    m.*
FROM
    review_messages m
WHERE
    m.thread_id = ANY (@thread_ids::uuid[])
ORDER BY
    m.thread_id,
    m.id;

-- name: DeleteReviewMessage :exec
WITH deleted AS (
    DELETE FROM review_messages m
    WHERE m.id = @id
    RETURNING
        m.thread_id
),
remaining AS (
    SELECT
        deleted.thread_id,
        MAX(
            CASE WHEN m.author_kind = 'human' THEN
                GREATEST(m.created_at, m.updated_at)
            END) AS human_activity_at,
        MAX(
            CASE WHEN m.author_kind = 'agent' THEN
                GREATEST(m.created_at, m.updated_at)
            END) AS agent_activity_at
    FROM
        deleted
        LEFT JOIN review_messages m ON m.thread_id = deleted.thread_id
            AND m.id <> @id
    GROUP BY
        deleted.thread_id)
UPDATE
    review_threads t
SET
    last_human_activity_at = remaining.human_activity_at,
    last_agent_activity_at = remaining.agent_activity_at
FROM
    remaining
WHERE
    t.id = remaining.thread_id;
