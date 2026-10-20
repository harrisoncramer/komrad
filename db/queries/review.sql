-- name: InsertReviewSnapshot :one
INSERT INTO review_snapshots(
    task_id,
    ordinal,
    base_sha,
    sha)
SELECT
    sqlc.arg('task_id')::uuid,
    COALESCE(MAX(r.ordinal), 0) + 1,
    @base_sha,
    @sha
FROM
    review_snapshots r
WHERE
    r.task_id = sqlc.arg('task_id')::uuid
RETURNING
    *;

-- name: GetReviewSnapshot :one
SELECT
    *
FROM
    review_snapshots r
WHERE
    r.id = @id;

-- name: GetLatestReviewSnapshot :one
SELECT
    *
FROM
    review_snapshots r
WHERE
    r.task_id = sqlc.arg('task_id')::uuid
ORDER BY
    r.ordinal DESC
LIMIT 1;

-- name: GetReviewSnapshotBySha :one
SELECT
    *
FROM
    review_snapshots r
WHERE
    r.task_id = sqlc.arg('task_id')::uuid
    AND r.sha = @sha
ORDER BY
    r.ordinal DESC
LIMIT 1;

-- name: ListReviewSnapshots :many
SELECT
    *
FROM
    review_snapshots r
WHERE
    r.task_id = sqlc.arg('task_id')::uuid
ORDER BY
    r.ordinal;

-- name: InsertReview :one
INSERT INTO reviews(
    task_id,
    snapshot_id,
    author_kind,
    author_agent_id)
VALUES (
    sqlc.arg('task_id')::uuid,
    @snapshot_id,
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

-- name: MoveOpenReview :one
UPDATE
    reviews v
SET
    snapshot_id = @snapshot_id
WHERE
    v.id = @id
    AND v.released_at IS NULL
RETURNING
    *;

-- name: GetOpenReview :one
SELECT
    *
FROM
    reviews v
WHERE
    v.task_id = sqlc.arg('task_id')::uuid
    AND v.author_kind = @author_kind
    AND v.author_agent_id IS NOT DISTINCT FROM sqlc.narg('author_agent_id')
    AND v.released_at IS NULL;

-- name: ListReviews :many
SELECT
    sqlc.embed(v),
    r.ordinal AS snapshot_ordinal,
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
    JOIN review_snapshots r ON r.id = v.snapshot_id
    LEFT JOIN mail d ON d.id = v.doorbell_mail_id
WHERE
    v.task_id = sqlc.arg('task_id')::uuid
ORDER BY
    r.ordinal,
    v.id;

-- name: ListWatchedReviews :many
SELECT
    v.id,
    v.task_id::uuid AS task_id,
    v.snapshot_id,
    v.author_kind,
    v.author_agent_id,
    v.verdict,
    v.released_at,
    v.created_at,
    v.updated_at,
    r.ordinal AS snapshot_ordinal,
    r.sha AS snapshot_sha,
    r.landed_at AS snapshot_landed_at,
    d.claimed_at AS delivered_at
FROM
    reviews v
    JOIN review_snapshots r ON r.id = v.snapshot_id
    LEFT JOIN mail d ON d.id = v.doorbell_mail_id
WHERE
    sqlc.narg('keys')::text[] IS NULL
    OR v.id = ANY (sqlc.narg('keys')::uuid[])
ORDER BY
    v.id;

-- name: ListReviewIDsOfTasks :many
SELECT
    v.id
FROM
    reviews v
WHERE
    v.task_id = ANY (sqlc.arg('task_ids')::uuid[])
ORDER BY
    v.id;

-- name: ListReviewThreadIDsOfTasks :many
SELECT
    t.id
FROM
    review_threads t
WHERE
    t.task_id = ANY (sqlc.arg('task_ids')::uuid[])
ORDER BY
    t.id;

-- name: CountOpenReviewComments :one
SELECT
    COUNT(*)
FROM
    review_messages m
WHERE
    m.review_id = @review_id;

-- name: ReleaseReview :one
UPDATE
    reviews
SET
    released_at = NOW(),
    verdict = @verdict,
    body = @body,
    path = @path
WHERE
    id = @id
RETURNING
    *;

-- name: ReopenRepliedThreads :execrows
UPDATE
    review_threads t
SET
    resolved_at = NULL,
    resolved_by_kind = NULL,
    resolved_by_agent_id = NULL
FROM
    reviews r
WHERE
    r.id = @review_id
    AND r.author_kind = 'human'
    AND t.resolved_at IS NOT NULL
    AND EXISTS (
        SELECT
            1
        FROM
            review_messages m
        WHERE
            m.thread_id = t.id
            AND m.review_id = r.id
            AND (t.resolved_by_kind IS DISTINCT FROM 'human'
                OR m.created_at > t.resolved_at));

-- name: SetReviewDoorbell :exec
UPDATE
    reviews
SET
    doorbell_mail_id = @doorbell_mail_id
WHERE
    id = @id;

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
        task_id,
        review_id,
        snapshot_id,
        base_sha,
        file_path,
        old_start,
        old_end,
        new_start,
        new_end,
        quoted_lines,
        forge_thread_id,
        last_agent_activity_at)
    VALUES (
        sqlc.arg('task_id')::uuid,
        @review_id,
        @snapshot_id,
        @base_sha,
        @file_path,
        @old_start,
        @old_end,
        @new_start,
        @new_end,
        @quoted_lines,
        sqlc.narg('forge_thread_id')::uuid,
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
        suggestion,
        external_login,
        forge_comment_id,
        forge_edited_at,
        held)
    SELECT
        o.id,
        @review_id,
        @author_kind::review_author_kind,
        sqlc.narg('author_agent_id'),
        @body,
        @suggestion,
        sqlc.narg('external_login')::text,
        sqlc.narg('forge_comment_id')::uuid,
        sqlc.narg('forge_edited_at')::timestamptz,
        @held
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
    r.ordinal AS snapshot_ordinal,
(t.last_agent_activity_at IS NOT NULL
        AND (t.human_read_at IS NULL
            OR t.last_agent_activity_at > t.human_read_at)) AS unread_by_human
FROM
    review_threads t
    JOIN reviews v ON v.id = t.review_id
    JOIN review_snapshots r ON r.id = t.snapshot_id
WHERE
    t.task_id = sqlc.arg('task_id')::uuid
ORDER BY
    r.ordinal,
    t.file_path,
    t.new_start,
    t.old_start,
    t.id;

-- name: ListWatchedReviewThreads :many
SELECT
    t.*,
    r.ordinal AS snapshot_ordinal,
(t.last_agent_activity_at IS NOT NULL
        AND (t.human_read_at IS NULL
            OR t.last_agent_activity_at > t.human_read_at)) AS unread_by_human
FROM
    review_threads t
    JOIN review_snapshots r ON r.id = t.snapshot_id
WHERE
    EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.task_id = t.task_id
            AND a.archived_at IS NULL)
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
        snapshot_id,
        author_kind,
        author_agent_id,
        body,
        suggestion,
        external_login,
        forge_comment_id,
        forge_edited_at,
        held,
        accepted_from)
    VALUES (
        @thread_id,
        sqlc.narg('review_id'),
        sqlc.narg('snapshot_id'),
        @author_kind,
        sqlc.narg('author_agent_id'),
        @body,
        @suggestion,
        sqlc.narg('external_login'),
        sqlc.narg('forge_comment_id'),
        sqlc.narg('forge_edited_at'),
        @held,
        sqlc.narg('accepted_from'))
RETURNING
    *
),
said AS (
    SELECT
        w.thread_id,
        w.author_kind,
        w.author_agent_id,
(w.snapshot_id IS NOT NULL
            AND v.author_kind = 'human') AS resolves,
(t.resolved_at IS NOT NULL
            AND w.author_kind = t.resolved_by_kind
            AND w.author_kind <> 'human'
            AND w.author_agent_id IS NOT DISTINCT FROM t.resolved_by_agent_id
            AND NOT EXISTS (
                SELECT
                    1
                FROM
                    agents a
                WHERE
                    a.id = w.author_agent_id
                    AND a.task_id = t.task_id
                    AND a.role = 'writer')) AS reopens
        FROM
            written w
            JOIN review_threads t ON t.id = w.thread_id
            JOIN reviews v ON v.id = t.review_id
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
        resolved_at = CASE WHEN s.resolves THEN
            NOW()
        WHEN s.reopens THEN
            NULL
        ELSE
            t.resolved_at
        END,
        resolved_by_kind = CASE WHEN s.resolves THEN
            s.author_kind
        WHEN s.reopens THEN
            NULL
        ELSE
            t.resolved_by_kind
        END,
        resolved_by_agent_id = CASE WHEN s.resolves THEN
            s.author_agent_id
        WHEN s.reopens THEN
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
            AND w.author_kind <> 'human'
            AND w.author_agent_id IS NOT DISTINCT FROM t.resolved_by_agent_id
            AND NOT EXISTS (
                SELECT
                    1
                FROM
                    agents a
                WHERE
                    a.id = w.author_agent_id
                    AND a.task_id = t.task_id
                    AND a.role = 'writer')) AS reopens
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
    task_id,
    file_path,
    fingerprint)
VALUES (
    sqlc.arg('task_id')::uuid,
    @file_path,
    @fingerprint)
ON CONFLICT (
    task_id,
    file_path)
    DO UPDATE SET
        fingerprint = @fingerprint,
        viewed_at = NOW()
    RETURNING
        *;

-- name: UnmarkReviewFileViewed :exec
DELETE FROM review_viewed_files
WHERE task_id = sqlc.arg('task_id')::uuid
    AND file_path = @file_path;

-- name: ListReviewViewedFiles :many
SELECT
    v.*
FROM
    review_viewed_files v
WHERE
    v.task_id = sqlc.arg('task_id')::uuid
ORDER BY
    v.file_path;

-- name: ListReviewTallies :many
WITH released AS (
    SELECT
        v.task_id::uuid AS task_id,
        COUNT(DISTINCT v.snapshot_id)::int AS snapshots
    FROM
        reviews v
    WHERE
        v.released_at IS NOT NULL
    GROUP BY
        v.task_id
),
concluded AS (
    SELECT DISTINCT ON (v.task_id)
        v.task_id,
        v.verdict,
        n.sha AS snapshot_sha
    FROM
        reviews v
        JOIN review_snapshots n ON n.id = v.snapshot_id
    WHERE
        v.released_at IS NOT NULL
        AND v.verdict IN ('approved', 'changes_requested')
    ORDER BY
        v.task_id,
        n.ordinal DESC,
        v.released_at DESC
)
SELECT
    r.task_id,
    r.snapshots,
    c.verdict,
    c.snapshot_sha
FROM
    released r
    LEFT JOIN concluded c ON c.task_id = r.task_id;

-- name: ListNewestTaskReviewSnapshots :many
SELECT DISTINCT ON (r.task_id)
    r.task_id::uuid AS task_id,
    r.sha
FROM
    review_snapshots r
ORDER BY
    r.task_id,
    r.ordinal DESC;

-- name: ListUnlandedReviewSnapshots :many
SELECT
    r.task_id::uuid AS task_id,
    r.ordinal,
    r.base_sha,
    r.sha,
    m.merged_at
FROM
    review_snapshots r
    LEFT JOIN LATERAL (
        SELECT
            p.merged_at
        FROM
            task_pull_requests o
            JOIN pull_requests p ON p.id = o.pull_request_id
        WHERE
            o.task_id = r.task_id
            AND p.merged_at IS NOT NULL
        ORDER BY
            p.merged_at DESC
        LIMIT 1) m ON TRUE
WHERE
    r.landed_at IS NULL
    AND EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.task_id = r.task_id
            AND a.archived_at IS NULL)
    AND EXISTS (
        SELECT
            1
        FROM
            reviews v
        WHERE
            v.snapshot_id = r.id
            AND v.released_at IS NOT NULL)
ORDER BY
    r.task_id,
    r.ordinal DESC;

-- name: LandReviewSnapshots :execrows
UPDATE
    review_snapshots
SET
    landed_at = NOW()
WHERE
    task_id = sqlc.arg('task_id')::uuid
    AND ordinal <= @ordinal
    AND landed_at IS NULL;

-- name: ListLiveReviewers :many
SELECT
    a.id
FROM
    agents a
WHERE
    a.task_id = sqlc.arg('task_id')::uuid
    AND a.role = 'reviewer'
    AND a.archived_at IS NULL
    AND a.state <> 'failed_to_create'
    AND a.state <> 'failed_to_cleanup'
    AND (
        SELECT
            v.verdict
        FROM
            reviews v
        WHERE
            v.task_id = a.task_id
            AND v.author_kind = 'agent'
            AND v.author_agent_id = a.id
            AND v.released_at IS NOT NULL
        ORDER BY
            v.released_at DESC
        LIMIT 1) IS DISTINCT FROM 'approved'
ORDER BY
    a.created_at,
    a.id;

-- name: InsertForgeReview :one
INSERT INTO reviews(
    task_id,
    snapshot_id,
    author_kind,
    external_login,
    released_at)
VALUES (
    sqlc.arg('task_id')::uuid,
    @snapshot_id,
    'forge',
    @external_login,
    NOW())
RETURNING
    *;

-- name: UpdateForgeReviewMessage :exec
UPDATE
    review_messages
SET
    body = @body,
    forge_edited_at = sqlc.narg('forge_edited_at'),
    held = @held
WHERE
    id = @id;

-- name: ListForgeMirroredMessages :many
SELECT
    t.id AS thread_id,
    t.forge_thread_id::uuid AS forge_thread_id,
    t.resolved_at,
    t.resolved_by_kind,
    t.forge_resolved,
    m.id AS message_id,
    m.forge_comment_id,
    m.forge_edited_at
FROM
    review_threads t
    LEFT JOIN review_messages m ON m.thread_id = t.id
        AND m.forge_comment_id IS NOT NULL
WHERE
    t.task_id = sqlc.arg('task_id')::uuid
    AND t.forge_thread_id IS NOT NULL
ORDER BY
    t.id,
    m.id;

-- name: ListForgeMirrorTasks :many
SELECT
    t.id AS task_id,
    COALESCE(t.cwd, '')::text AS cwd,
    r.id AS snapshot_id,
    r.base_sha,
    r.sha AS snapshot_sha
FROM
    task_pull_requests o
    JOIN tasks t ON t.id = o.task_id
    JOIN LATERAL (
        SELECT
            *
        FROM
            review_snapshots r
        WHERE
            r.task_id = t.id
        ORDER BY
            r.ordinal DESC
        LIMIT 1) r ON TRUE
WHERE
    o.pull_request_id = sqlc.arg('pull_request_id')
    AND EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.task_id = t.id
            AND a.archived_at IS NULL
            AND a.deleting_at IS NULL)
ORDER BY
    t.id;

-- name: SetForgeThreadResolved :exec
UPDATE
    review_threads
SET
    forge_resolved = @forge_resolved
WHERE
    id = @id;
