-- name: ListCommentSyncPullRequests :many
SELECT
    p.id,
    p.host,
    p.forge,
    p.repository_url,
    p.number,
    p.base_sha,
    p.comments_mark,
    p.comments_read_at
FROM
    pull_requests p
WHERE
    p.merged_at IS NULL
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
    p.host,
    p.repository_url,
    p.number;

-- name: MarkPullRequestCommentsRead :exec
UPDATE
    pull_requests
SET
    comments_mark = @comments_mark,
    comments_read_at = @read_at
WHERE
    id = @id;

-- name: UpsertForgeReviewThread :exec
INSERT INTO forge_review_threads(
    pull_request_id,
    provider_id,
    path,
    side,
    commit_sha,
    base_sha,
    start_line,
    end_line,
    diff_hunk,
    quoted_lines,
    resolved,
    outdated,
    written_at)
VALUES (
    @pull_request_id,
    @provider_id,
    @path,
    @side,
    @commit_sha,
    @base_sha,
    sqlc.narg('start_line'),
    sqlc.narg('end_line'),
    @diff_hunk,
    @quoted_lines::text[],
    @resolved,
    @outdated,
    @written_at)
ON CONFLICT (
    engine_id,
    pull_request_id,
    provider_id)
    DO UPDATE SET
        path = excluded.path,
        side = excluded.side,
        commit_sha = excluded.commit_sha,
        base_sha = excluded.base_sha,
        start_line = excluded.start_line,
        end_line = excluded.end_line,
        diff_hunk = excluded.diff_hunk,
        quoted_lines = excluded.quoted_lines,
        resolved = excluded.resolved,
        outdated = excluded.outdated,
        written_at = excluded.written_at
    WHERE (forge_review_threads.path,
        forge_review_threads.side,
        forge_review_threads.commit_sha,
        forge_review_threads.base_sha,
        forge_review_threads.start_line,
        forge_review_threads.end_line,
        forge_review_threads.diff_hunk,
        forge_review_threads.quoted_lines,
        forge_review_threads.resolved,
        forge_review_threads.outdated,
        forge_review_threads.written_at)
    IS DISTINCT FROM (excluded.path,
        excluded.side,
        excluded.commit_sha,
        excluded.base_sha,
        excluded.start_line,
        excluded.end_line,
        excluded.diff_hunk,
        excluded.quoted_lines,
        excluded.resolved,
        excluded.outdated,
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
    body,
    written_at,
    edited_at)
VALUES (
    @thread_id,
    @provider_id,
    @author_login,
    @author_is_bot,
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
        body = excluded.body,
        written_at = excluded.written_at,
        edited_at = excluded.edited_at
    WHERE (forge_review_comments.author_login,
        forge_review_comments.author_is_bot,
        forge_review_comments.body,
        forge_review_comments.written_at,
        forge_review_comments.edited_at)
    IS DISTINCT FROM (excluded.author_login,
        excluded.author_is_bot,
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
