-- name: InsertAgentForgeReply :one
INSERT INTO review_forge_replies(
    thread_id,
    state,
    author_kind,
    author_agent_id,
    proposed_body,
    body)
SELECT
    t.id,
    'draft',
    'agent',
    @author_agent_id::uuid,
    @body::text,
    @body::text
FROM
    review_threads t
WHERE
    t.id = @thread_id
    AND t.forge_thread_id IS NOT NULL
RETURNING
    *;

-- name: InsertHumanForgeReply :one
INSERT INTO review_forge_replies(
    thread_id,
    state,
    author_kind,
    body,
    approver_kind,
    approved_at)
SELECT
    t.id,
    'approved',
    'human',
    @body::text,
    'human',
    NOW()
FROM
    review_threads t
WHERE
    t.id = @thread_id
    AND t.forge_thread_id IS NOT NULL
RETURNING
    *;

-- name: MarkForgeReplyApproved :one
UPDATE
    review_forge_replies
SET
    state = 'approved',
    body = @body,
    approver_kind = @approver_kind::forge_reply_approver_kind,
    approved_at = NOW()
WHERE
    id = @id
    AND state = 'draft'
RETURNING
    *;

-- name: BeginForgeReplySend :one
UPDATE
    review_forge_replies
SET
    state = 'sending',
    sending_started_at = NOW(),
    error = NULL
WHERE
    id = @id
    AND state IN ('approved', 'failed')
RETURNING
    *;

-- name: MarkForgeReplySent :one
UPDATE
    review_forge_replies
SET
    state = 'sent',
    sent_at = NOW(),
    forge_comment_provider_id = @forge_comment_provider_id::text
WHERE
    id = @id
    AND state = 'sending'
RETURNING
    *;

-- name: MarkForgeReplyFailed :one
UPDATE
    review_forge_replies
SET
    state = 'failed',
    error = @error::text
WHERE
    id = @id
    AND state = 'sending'
RETURNING
    *;

-- name: DeleteForgeReply :execrows
DELETE FROM review_forge_replies
WHERE id = @id
    AND state IN ('draft', 'failed');

-- name: GetForgeReply :one
SELECT
    *
FROM
    review_forge_replies
WHERE
    id = @id;

-- name: ListForgeReplies :many
SELECT
    sqlc.embed(r),
    c.id AS mirrored_comment_id
FROM
    review_forge_replies r
    JOIN review_threads t ON t.id = r.thread_id
    LEFT JOIN forge_review_comments c ON c.thread_id = t.forge_thread_id
        AND c.provider_id = r.forge_comment_provider_id
WHERE
    r.thread_id = ANY (@thread_ids::uuid[])
ORDER BY
    r.thread_id,
    r.id;

-- name: ListSendingForgeReplies :many
SELECT
    *
FROM
    review_forge_replies
WHERE
    state = 'sending'
ORDER BY
    id;

-- name: GetForgeReplyTarget :one
SELECT
    f.provider_id,
    f.path,
    p.repository_url,
    p.number
FROM
    review_threads t
    JOIN forge_review_threads f ON f.id = t.forge_thread_id
    JOIN pull_requests p ON p.id = f.pull_request_id
WHERE
    t.id = @thread_id;
