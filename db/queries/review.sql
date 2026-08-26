-- name: InsertReviewThread :one
INSERT INTO review_threads(
    agent_id,
    file_path,
    old_start,
    old_end,
    new_start,
    new_end,
    quoted_lines)
VALUES (
    @agent_id,
    @file_path,
    @old_start,
    @old_end,
    @new_start,
    @new_end,
    @quoted_lines)
RETURNING
    *;

-- name: ListReviewThreads :many
SELECT
    t.*,
(t.agent_read_at IS NULL
        OR t.last_human_activity_at > t.agent_read_at) AS unread_by_agent,
    EXISTS (
        SELECT
            1
        FROM
            review_messages m
        WHERE
            m.thread_id = t.id
            AND m.author_kind = 'agent'
            AND (t.human_read_at IS NULL
                OR m.created_at > t.human_read_at)) AS unread_by_human
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

-- name: SetReviewThreadResolved :one
UPDATE
    review_threads
SET
    resolved_at = sqlc.narg('resolved_at')
WHERE
    id = @id
RETURNING
    *;

-- name: TouchReviewThread :exec
UPDATE
    review_threads
SET
    last_human_activity_at = CASE WHEN @human::boolean THEN
        NOW()
    ELSE
        last_human_activity_at
    END,
    resolved_at = NULL
WHERE
    id = @id;

-- name: MarkReviewThreadsReadByAgent :exec
UPDATE
    review_threads
SET
    agent_read_at = @read_at
WHERE
    id = ANY (@ids::uuid[]);

-- name: MarkReviewThreadsReadByHuman :exec
UPDATE
    review_threads
SET
    human_read_at = @read_at
WHERE
    id = ANY (@ids::uuid[]);

-- name: CountUnreadReviewThreads :one
SELECT
    COUNT(*)
FROM
    review_threads t
WHERE
    t.agent_id = @agent_id
    AND t.resolved_at IS NULL
    AND (t.agent_read_at IS NULL
        OR t.last_human_activity_at > t.agent_read_at);

-- name: DeleteReviewThread :exec
DELETE FROM review_threads
WHERE id = @id;

-- name: InsertReviewMessage :one
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
    *;

-- name: UpdateReviewMessageBody :one
UPDATE
    review_messages
SET
    body = @body
WHERE
    id = @id
RETURNING
    *;

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
DELETE FROM review_messages
WHERE id = @id;
