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

-- name: GetReviewRoundBySnapshot :one
SELECT
    *
FROM
    review_rounds r
WHERE
    r.agent_id = @agent_id
    AND r.snapshot_sha = @snapshot_sha
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
    sqlc.embed(v),
(
        SELECT
            COUNT(*)
        FROM
            review_threads t
        WHERE
            t.review_id = v.id
            AND t.resolved_at IS NULL)::int AS open_threads
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
    sqlc.embed(v),
    r.ordinal AS round_ordinal,
    d.claimed_at AS delivered_at,
(
        SELECT
            COUNT(*)
        FROM
            review_threads t
        WHERE
            t.review_id = v.id
            AND t.resolved_at IS NULL)::int AS open_threads
FROM
    reviews v
    JOIN review_rounds r ON r.id = v.round_id
    LEFT JOIN mail d ON d.id = v.doorbell_mail_id
WHERE
    v.agent_id = @agent_id
ORDER BY
    r.ordinal,
    v.id;

-- name: ListWatchedReviews :many
SELECT
    v.id,
    v.agent_id,
    v.round_id,
    v.author_kind,
    v.author_agent_id,
    v.verdict,
    v.released_at,
    v.created_at,
    v.updated_at,
    r.ordinal AS round_ordinal,
    r.snapshot_sha AS round_snapshot_sha,
    d.claimed_at AS delivered_at
FROM
    reviews v
    JOIN review_rounds r ON r.id = v.round_id
    LEFT JOIN mail d ON d.id = v.doorbell_mail_id
WHERE
    sqlc.narg('keys')::text[] IS NULL
    OR v.id = ANY (sqlc.narg('keys')::uuid[])
ORDER BY
    v.id;

-- name: ListReviewsOfAgents :many
SELECT
    v.id,
    v.agent_id,
    v.round_id,
    v.author_kind,
    v.author_agent_id,
    v.verdict,
    v.released_at,
    v.created_at,
    v.updated_at,
    r.ordinal AS round_ordinal,
    r.snapshot_sha AS round_snapshot_sha,
    d.claimed_at AS delivered_at
FROM
    reviews v
    JOIN review_rounds r ON r.id = v.round_id
    LEFT JOIN mail d ON d.id = v.doorbell_mail_id
WHERE
    v.agent_id = ANY (@agent_ids::uuid[])
ORDER BY
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
    body = @body,
    path = @path,
    doorbell_mail_id = sqlc.narg('doorbell_mail_id')
WHERE
    v.id = @id
RETURNING
    *;

-- name: DeleteOpenReviewsByAuthor :exec
DELETE FROM reviews
WHERE author_agent_id = ANY (@author_agent_ids::uuid[])
    AND released_at IS NULL;

-- name: DeleteReview :exec
DELETE FROM reviews
WHERE id = @id;

-- name: InsertReviewThreadWithMessage :one
WITH opened AS (
    INSERT INTO review_threads(
        agent_id,
        review_id,
        round_id,
        base_sha,
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
        @round_id,
        @base_sha,
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
    v.released_at AS review_released_at,
    v.author_agent_id AS review_author_agent_id,
    r.ordinal AS round_ordinal,
(t.last_agent_activity_at IS NOT NULL
        AND (t.human_read_at IS NULL
            OR t.last_agent_activity_at > t.human_read_at)) AS unread_by_human
FROM
    review_threads t
    JOIN reviews v ON v.id = t.review_id
    JOIN review_rounds r ON r.id = t.round_id
WHERE
    t.agent_id = @agent_id
ORDER BY
    r.ordinal,
    t.file_path,
    t.new_start,
    t.old_start,
    t.id;

-- name: ListWatchedReviewThreads :many
SELECT
    t.*,
    r.ordinal AS round_ordinal,
(t.last_agent_activity_at IS NOT NULL
        AND (t.human_read_at IS NULL
            OR t.last_agent_activity_at > t.human_read_at)) AS unread_by_human
FROM
    review_threads t
    JOIN review_rounds r ON r.id = t.round_id
    JOIN agents a ON a.id = t.agent_id
WHERE
    a.archived_at IS NULL
    AND (sqlc.narg('keys')::text[] IS NULL
        OR t.id = ANY (sqlc.narg('keys')::uuid[]))
ORDER BY
    t.id;

-- name: ListReviewMessagesOfThreads :many
SELECT
    m.*
FROM
    review_messages m
WHERE
    m.thread_id = ANY (@thread_ids::uuid[])
ORDER BY
    m.thread_id,
    m.id;

-- name: GetReviewThread :one
SELECT
    t.*,
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
    END,
    resolved_by_kind = CASE WHEN @resolved::boolean THEN
        sqlc.narg('resolved_by_kind')::review_author_kind
    ELSE
        NULL
    END,
    resolved_by_agent_id = CASE WHEN @resolved::boolean THEN
        sqlc.narg('resolved_by_agent_id')::uuid
    ELSE
        NULL
    END
WHERE
    t.id = @id
RETURNING
    *;

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
said AS (
    SELECT
        w.thread_id,
        w.author_kind,
(t.resolved_at IS NOT NULL
            AND w.author_kind = t.resolved_by_kind
            AND w.author_agent_id IS NOT DISTINCT FROM t.resolved_by_agent_id) AS reopens
    FROM
        written w
        JOIN review_threads t ON t.id = w.thread_id
),
touched AS (
    UPDATE
        review_threads t
    SET
        last_agent_activity_at = CASE WHEN s.author_kind = 'agent' THEN
            NOW()
        ELSE
            t.last_agent_activity_at
        END,
        resolved_at = CASE WHEN s.reopens THEN
            NULL
        ELSE
            t.resolved_at
        END,
        resolved_by_kind = CASE WHEN s.reopens THEN
            NULL
        ELSE
            t.resolved_by_kind
        END,
        resolved_by_agent_id = CASE WHEN s.reopens THEN
            NULL
        ELSE
            t.resolved_by_agent_id
        END
    FROM
        said s
    WHERE
        t.id = s.thread_id
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
said AS (
    SELECT
        w.thread_id,
        w.author_kind,
(t.resolved_at IS NOT NULL
            AND w.author_kind = t.resolved_by_kind
            AND w.author_agent_id IS NOT DISTINCT FROM t.resolved_by_agent_id) AS reopens
    FROM
        written w
        JOIN review_threads t ON t.id = w.thread_id
),
touched AS (
    UPDATE
        review_threads t
    SET
        last_agent_activity_at = CASE WHEN s.author_kind = 'agent' THEN
            NOW()
        ELSE
            t.last_agent_activity_at
        END,
        resolved_at = CASE WHEN s.reopens THEN
            NULL
        ELSE
            t.resolved_at
        END,
        resolved_by_kind = CASE WHEN s.reopens THEN
            NULL
        ELSE
            t.resolved_by_kind
        END,
        resolved_by_agent_id = CASE WHEN s.reopens THEN
            NULL
        ELSE
            t.resolved_by_agent_id
        END
    FROM
        said s
    WHERE
        t.id = s.thread_id
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
        fingerprint = @fingerprint,
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

-- name: ListReviewTallies :many
WITH released AS (
    SELECT
        v.agent_id,
        COUNT(DISTINCT v.round_id)::int AS rounds
    FROM
        reviews v
    WHERE
        v.released_at IS NOT NULL
    GROUP BY
        v.agent_id
),
concluded AS (
    SELECT DISTINCT ON (v.agent_id)
        v.agent_id,
        v.verdict,
        n.snapshot_sha
    FROM
        reviews v
        JOIN review_rounds n ON n.id = v.round_id
    WHERE
        v.released_at IS NOT NULL
        AND v.verdict IN ('approved', 'changes_requested')
    ORDER BY
        v.agent_id,
        n.ordinal DESC,
        v.released_at DESC
)
SELECT
    r.agent_id,
    r.rounds,
    c.verdict,
    c.snapshot_sha
FROM
    released r
    LEFT JOIN concluded c ON c.agent_id = r.agent_id;

-- name: ListNewestReviewRounds :many
SELECT DISTINCT ON (agent_id)
    agent_id,
    snapshot_sha
FROM
    review_rounds
ORDER BY
    agent_id,
    ordinal DESC;

-- name: ListLiveReviewers :many
SELECT
    c.parent_agent_id AS agent_id,
    c.id AS reviewer_id
FROM
    agents c
WHERE
    c.role = 'reviewer'
    AND c.parent_agent_id IS NOT NULL
    AND c.archived_at IS NULL
    AND c.state <> 'failed_to_create'
    AND c.state <> 'failed_to_cleanup'
    AND (
        SELECT
            v.verdict
        FROM
            reviews v
        WHERE
            v.agent_id = c.parent_agent_id
            AND v.author_kind = 'agent'
            AND v.author_agent_id = c.id
            AND v.released_at IS NOT NULL
        ORDER BY
            v.released_at DESC
        LIMIT 1) IS DISTINCT FROM 'approved';
