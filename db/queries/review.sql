-- name: InsertReviewRound :one
INSERT INTO review_rounds(
    agent_id,
    ordinal,
    base_sha,
    snapshot_sha)
SELECT
    @agent_id,
    COALESCE(MAX(r.ordinal), 0) + 1,
    @base_sha,
    @snapshot_sha
FROM
    review_rounds r
WHERE
    r.agent_id = @agent_id
RETURNING
    *;

-- name: GetReviewRound :one
SELECT
    *
FROM
    review_rounds r
WHERE
    r.id = @id;

-- name: GetLatestReviewRound :one
SELECT
    *
FROM
    review_rounds r
WHERE
    r.agent_id = @agent_id
ORDER BY
    r.ordinal DESC
LIMIT 1;

-- name: ListReviewRounds :many
SELECT
    *
FROM
    review_rounds r
WHERE
    r.agent_id = @agent_id
ORDER BY
    r.ordinal;

-- name: DeleteReviewRound :exec
DELETE FROM review_rounds
WHERE id = @id;

-- name: InsertReview :one
INSERT INTO reviews(
    agent_id,
    round_id,
    author_kind,
    author_agent_id)
VALUES (
    @agent_id,
    @round_id,
    @author_kind,
    sqlc.narg('author_agent_id'))
RETURNING
    *;

-- name: GetReview :one
SELECT
    *
FROM
    reviews v
WHERE
    v.id = @id;

-- name: GetOpenReview :one
SELECT
    *
FROM
    reviews v
WHERE
    v.agent_id = @agent_id
    AND v.author_kind = @author_kind
    AND v.author_agent_id IS NOT DISTINCT FROM sqlc.narg('author_agent_id')
    AND v.released_at IS NULL;

-- name: GetLastReleasedReview :one
SELECT
    *
FROM
    reviews v
WHERE
    v.agent_id = @agent_id
    AND v.author_kind = @author_kind
    AND v.author_agent_id IS NOT DISTINCT FROM sqlc.narg('author_agent_id')
    AND v.released_at IS NOT NULL
ORDER BY
    v.released_at DESC
LIMIT 1;

-- name: ListReviews :many
SELECT
    v.*,
    r.ordinal AS round_ordinal,
    d.claimed_at AS delivered_at
FROM
    reviews v
    JOIN review_rounds r ON r.id = v.round_id
    LEFT JOIN mail d ON d.id = v.doorbell_mail_id
WHERE
    v.agent_id = @agent_id
ORDER BY
    r.ordinal,
    v.id;

-- name: CountOpenReviewComments :one
SELECT
    COUNT(*)
FROM
    review_messages m
WHERE
    m.review_id = @review_id;

-- name: ReleaseReview :one
UPDATE
    reviews v
SET
    released_at = NOW(),
    verdict = @verdict,
    path = @path,
    doorbell_mail_id = sqlc.narg('doorbell_mail_id')
WHERE
    v.id = @id
RETURNING
    *;

-- name: PointReviewAtDoorbell :one
UPDATE
    reviews v
SET
    doorbell_mail_id = @doorbell_mail_id
WHERE
    v.id = @id
RETURNING
    *;

-- name: DeleteReview :exec
DELETE FROM reviews
WHERE id = @id;

-- name: InsertReviewThreadWithMessage :one
WITH opened AS (
    INSERT INTO review_threads(
        agent_id,
        review_id,
        file_path,
        old_start,
        old_end,
        new_start,
        new_end,
        quoted_lines,
        last_agent_activity_at)
    VALUES (
        @agent_id,
        @review_id,
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
        review_id,
        author_kind,
        author_agent_id,
        body,
        suggestion)
    SELECT
        o.id,
        @review_id,
        @author_kind::review_author_kind,
        sqlc.narg('author_agent_id'),
        @body,
        @suggestion
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
    v.round_id,
    v.released_at AS review_released_at,
    r.ordinal AS round_ordinal,
(t.last_agent_activity_at IS NOT NULL
        AND (t.human_read_at IS NULL
            OR t.last_agent_activity_at > t.human_read_at)) AS unread_by_human
FROM
    review_threads t
    JOIN reviews v ON v.id = t.review_id
    JOIN review_rounds r ON r.id = v.round_id
WHERE
    t.agent_id = @agent_id
ORDER BY
    r.ordinal,
    t.file_path,
    t.new_start,
    t.old_start,
    t.id;

-- name: GetReviewThread :one
SELECT
    t.*,
    v.round_id,
    v.released_at AS review_released_at,
    v.author_kind AS review_author_kind,
    v.author_agent_id AS review_author_agent_id
FROM
    review_threads t
    JOIN reviews v ON v.id = t.review_id
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
        review_id,
        author_kind,
        author_agent_id,
        body,
        suggestion)
    VALUES (
        @thread_id,
        sqlc.narg('review_id'),
        @author_kind,
        sqlc.narg('author_agent_id'),
        @body,
        @suggestion)
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
        body = @body,
        suggestion = @suggestion
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
SELECT
    m.*,
    v.released_at AS review_released_at,
    d.claimed_at AS delivered_at
FROM
    review_messages m
    LEFT JOIN reviews v ON v.id = m.review_id
    LEFT JOIN mail d ON d.id = v.doorbell_mail_id
WHERE
    m.thread_id = ANY (@thread_ids::uuid[])
ORDER BY
    m.thread_id,
    m.id;

-- name: DeleteReviewMessage :exec
DELETE FROM review_messages
WHERE id = @id;

-- name: MarkReviewFileViewed :one
INSERT INTO review_viewed_files(
    round_id,
    file_path)
VALUES (
    @round_id,
    @file_path)
ON CONFLICT (
    round_id,
    file_path)
    DO UPDATE SET
        viewed_at = NOW()
    RETURNING
        *;

-- name: UnmarkReviewFileViewed :exec
DELETE FROM review_viewed_files
WHERE round_id = @round_id
    AND file_path = @file_path;

-- name: ListReviewViewedFiles :many
SELECT
    v.*
FROM
    review_viewed_files v
WHERE
    v.round_id = @round_id
ORDER BY
    v.file_path;
