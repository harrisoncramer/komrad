-- name: InsertAgentForgeReply :one
INSERT INTO review_forge_replies(thread_id, state, author_kind, author_agent_id, proposed_body, body)
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
INSERT INTO review_forge_replies(thread_id, state, author_kind, body, approver_kind, approved_at)
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

-- name: ApproveForgeReply :one
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

-- name: DiscardForgeReply :execrows
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
    *
FROM
    review_forge_replies
WHERE
    thread_id = ANY (@thread_ids::uuid[])
ORDER BY
    thread_id,
    id;

-- name: ListSendingForgeReplies :many
SELECT
    *
FROM
    review_forge_replies
WHERE
    state = 'sending'
ORDER BY
    id;
