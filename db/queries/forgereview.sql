-- name: ListCommentSyncPullRequests :many
SELECT
    p.id,
    p.repository_url,
    p.number
FROM
    pull_requests p
WHERE
    p.forge = 'github'
    AND p.merged_at IS NULL
    AND p.closed_at IS NULL
    AND EXISTS (
        SELECT
            1
        FROM
            agent_pull_requests o
            JOIN agents a ON a.id = o.agent_id
        WHERE
            o.pull_request_id = p.id
            AND a.archived_at IS NULL
            AND a.deleting_at IS NULL)
ORDER BY
    p.repository_url,
    p.number;

-- name: UpsertForgeReviewThread :exec
INSERT INTO forge_review_threads(
    pull_request_id,
    provider_id,
    path,
    line,
    old_side,
    commit_sha,
    quote,
    url,
    resolved,
    written_at)
VALUES (
    @pull_request_id,
    @provider_id,
    @path,
    sqlc.narg('line'),
    @old_side,
    @commit_sha,
    @quote,
    @url,
    @resolved,
    @written_at)
ON CONFLICT (
    engine_id,
    pull_request_id,
    provider_id)
    DO UPDATE SET
        path = excluded.path,
        line = excluded.line,
        old_side = excluded.old_side,
        commit_sha = excluded.commit_sha,
        quote = excluded.quote,
        url = excluded.url,
        resolved = excluded.resolved,
        written_at = excluded.written_at
    WHERE (
        forge_review_threads.path, forge_review_threads.line, forge_review_threads.old_side, forge_review_threads.commit_sha, forge_review_threads.quote, forge_review_threads.url, forge_review_threads.resolved, forge_review_threads.written_at) IS DISTINCT FROM (
        excluded.path,
        excluded.line,
        excluded.old_side,
        excluded.commit_sha,
        excluded.quote,
        excluded.url,
        excluded.resolved,
        excluded.written_at);

-- name: ListForgeReviewThreadIDs :many
SELECT
    t.id,
    t.provider_id
FROM
    forge_review_threads t
WHERE
    t.pull_request_id = @pull_request_id
ORDER BY
    t.id;

-- name: DeleteForgeReviewThreadsExcept :exec
DELETE FROM forge_review_threads
WHERE pull_request_id = @pull_request_id
    AND NOT (provider_id = ANY (@kept::text[]));

-- name: UpsertForgeReviewComment :exec
INSERT INTO forge_review_comments(
    thread_id,
    provider_id,
    author_login,
    author_is_bot,
    yours,
    body,
    written_at,
    edited_at)
VALUES (
    @thread_id,
    @provider_id,
    @author_login,
    @author_is_bot,
    @yours,
    @body,
    @written_at,
    sqlc.narg('edited_at'))
ON CONFLICT (
    engine_id,
    thread_id,
    provider_id)
    DO UPDATE SET
        author_login = excluded.author_login,
        author_is_bot = excluded.author_is_bot,
        yours = excluded.yours,
        body = excluded.body,
        written_at = excluded.written_at,
        edited_at = excluded.edited_at
    WHERE (
        forge_review_comments.author_login, forge_review_comments.author_is_bot, forge_review_comments.yours, forge_review_comments.body, forge_review_comments.written_at, forge_review_comments.edited_at) IS DISTINCT FROM (
        excluded.author_login,
        excluded.author_is_bot,
        excluded.yours,
        excluded.body,
        excluded.written_at,
        excluded.edited_at);

-- name: DeleteForgeReviewCommentsExcept :exec
DELETE FROM forge_review_comments
WHERE thread_id = @thread_id
    AND NOT (provider_id = ANY (@kept::text[]));

-- name: ListWatchedForgeReviewThreads :many
SELECT
    t.*
FROM
    forge_review_threads t
WHERE
    EXISTS (
        SELECT
            1
        FROM
            agent_pull_requests o
        WHERE
            o.pull_request_id = t.pull_request_id)
    AND (sqlc.narg('keys')::text[] IS NULL
        OR t.id = ANY (sqlc.narg('keys')::uuid[]))
ORDER BY
    t.id;

-- name: ListForgeReviewCommentsOfThreads :many
SELECT
    c.*
FROM
    forge_review_comments c
WHERE
    c.thread_id = ANY (@thread_ids::uuid[])
ORDER BY
    c.thread_id,
    c.written_at,
    c.id;

-- name: SetForgeReviewThreadDismissed :one
UPDATE
    forge_review_threads t
SET
    dismissed_at = CASE WHEN @dismissed::boolean THEN
        NOW()
    END
WHERE
    t.id = @id
RETURNING
    t.id;
