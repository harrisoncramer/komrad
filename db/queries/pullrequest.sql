-- name: UpsertPullRequest :one
WITH prior AS (
    SELECT
        held.state
    FROM
        pull_requests held
    WHERE
        held.host = @host
        AND held.provider_id = @provider_id
),
written AS (
    INSERT INTO pull_requests(
        host,
        forge,
        provider_id,
        url,
        repository_url,
        head_repository_url,
        number,
        title,
        head_branch,
        head_sha,
        base_branch,
        base_sha,
        draft,
        state,
        opened_at,
        changed_at,
        merged_at,
        closed_at)
    VALUES (
        @host,
        @forge,
        @provider_id,
        @url,
        @repository_url,
        @head_repository_url,
        @number,
        @title,
        @head_branch,
        @head_sha,
        @base_branch,
        @base_sha,
        @draft,
        @state,
        @opened_at,
        @changed_at,
        sqlc.narg('merged_at'),
        sqlc.narg('closed_at'))
ON CONFLICT (
    engine_id,
    host,
    provider_id)
    DO UPDATE SET
        url = EXCLUDED.url,
        repository_url = EXCLUDED.repository_url,
        head_repository_url = EXCLUDED.head_repository_url,
        number = EXCLUDED.number,
        title = EXCLUDED.title,
        head_branch = EXCLUDED.head_branch,
        head_sha = EXCLUDED.head_sha,
        base_branch = EXCLUDED.base_branch,
        base_sha = EXCLUDED.base_sha,
        draft = EXCLUDED.draft,
        state = EXCLUDED.state,
        opened_at = EXCLUDED.opened_at,
        changed_at = EXCLUDED.changed_at,
        merged_at = EXCLUDED.merged_at,
        closed_at = EXCLUDED.closed_at
    WHERE (
        pull_requests.url,
        pull_requests.repository_url,
        pull_requests.head_repository_url,
        pull_requests.number,
        pull_requests.title,
        pull_requests.head_branch,
        pull_requests.head_sha,
        pull_requests.base_branch,
        pull_requests.base_sha,
        pull_requests.draft,
        pull_requests.state,
        pull_requests.opened_at,
        pull_requests.changed_at,
        pull_requests.merged_at,
        pull_requests.closed_at) IS DISTINCT FROM (
        EXCLUDED.url,
        EXCLUDED.repository_url,
        EXCLUDED.head_repository_url,
        EXCLUDED.number,
        EXCLUDED.title,
        EXCLUDED.head_branch,
        EXCLUDED.head_sha,
        EXCLUDED.base_branch,
        EXCLUDED.base_sha,
        EXCLUDED.draft,
        EXCLUDED.state,
        EXCLUDED.opened_at,
        EXCLUDED.changed_at,
        EXCLUDED.merged_at,
        EXCLUDED.closed_at)
RETURNING
    id,
    first_seen_at = NOW() AS inserted)
SELECT
    written.id,
    written.inserted,
    TRUE AS changed,
    prior.state AS old_state
FROM
    written
    LEFT JOIN prior ON TRUE
UNION ALL
SELECT
    pull_requests.id,
    FALSE AS inserted,
    FALSE AS changed,
    NULL::text AS old_state
FROM
    pull_requests
WHERE
    pull_requests.host = @host
    AND pull_requests.provider_id = @provider_id
    AND NOT EXISTS (
        SELECT
            1
        FROM
            written);

-- name: GetPullRequestByIdentity :one
SELECT
    *
FROM
    pull_requests
WHERE
    host = @host
    AND provider_id = @provider_id;

-- name: ObserveAgentPullRequests :execrows
INSERT INTO agent_pull_requests(
    agent_id,
    pull_request_id,
    first_seen_at,
    last_seen_at)
SELECT
    a.id,
    p.id,
    NOW(),
    NOW()
FROM
    agents a
    JOIN pull_requests p ON p.repository_url = a.repository_url
        AND p.head_repository_url = a.repository_url
        AND p.head_branch = a.branch
    JOIN workflows w ON w.id = a.workflow_id
    LEFT JOIN workflow_provisioner_configs c ON c.id = w.provisioner_config_id
WHERE
    a.deleting_at IS NULL
    AND a.repository_url IS NOT NULL
    AND a.branch IS NOT NULL
    AND a.branch <> ''
    AND (c.worktree_base_branch IS NULL
        OR a.branch <> c.worktree_base_branch)
ON CONFLICT (agent_id,
    pull_request_id)
    DO UPDATE SET
        last_seen_at = NOW()
    WHERE
        agent_pull_requests.last_seen_at < NOW() - interval '1 hour';

-- name: ListAgentPullRequests :many
SELECT
    o.agent_id,
    o.pull_request_id,
    o.first_seen_at,
    o.last_seen_at,
    o.engine_id
FROM
    agent_pull_requests o
WHERE
    o.agent_id = ANY (@agent_ids::uuid[])
ORDER BY
    o.first_seen_at;

-- name: ListLiveRepositories :many
SELECT DISTINCT
    repository_url
FROM
    agents
WHERE
    archived_at IS NULL
    AND repository_url IS NOT NULL
    AND repository_url <> ''
ORDER BY
    repository_url;

-- name: ListSyncTargets :many
SELECT DISTINCT
    a.repository_url::text AS repository_url,
    a.branch::text AS branch,
    COALESCE(a.head_sha, '')::text AS head_sha
FROM
    agents a
    JOIN workflows w ON w.id = a.workflow_id
    LEFT JOIN workflow_provisioner_configs c ON c.id = w.provisioner_config_id
WHERE
    a.archived_at IS NULL
    AND a.repository_url IS NOT NULL
    AND a.repository_url <> ''
    AND a.branch IS NOT NULL
    AND a.branch <> ''
    AND (c.worktree_base_branch IS NULL
        OR a.branch <> c.worktree_base_branch)
ORDER BY
    repository_url,
    branch,
    head_sha;

-- name: EnsurePullRequestSync :exec
INSERT INTO pull_request_syncs(
    repository_url)
VALUES (
    @repository_url)
ON CONFLICT (
    engine_id,
    repository_url)
    DO NOTHING;

-- name: GetPullRequestForUpdate :one
SELECT
    *
FROM
    pull_requests
WHERE
    id = @id
FOR UPDATE;

-- name: WriteStanding :exec
UPDATE
    pull_requests
SET
    read_decision = sqlc.narg('read_decision')::text,
    read_checks = sqlc.narg('read_checks')::text,
    read_mergeability = sqlc.narg('read_mergeability')::text,
    read_head_sha = sqlc.narg('read_head_sha')::text,
    known_checks = sqlc.narg('known_checks')::text,
    known_checks_sha = sqlc.narg('known_checks_sha')::text,
    known_mergeability = sqlc.narg('known_mergeability')::text,
    known_mergeability_sha = sqlc.narg('known_mergeability_sha')::text,
    mergeability_blank_since = sqlc.narg('mergeability_blank_since')::timestamptz,
    known_decision = sqlc.narg('known_decision')::text,
    auto_merge = sqlc.narg('auto_merge')::boolean,
    auto_mergeable = @auto_mergeable::boolean
WHERE
    id = @id
    AND (read_decision,
        read_checks,
        read_mergeability,
        read_head_sha,
        known_checks,
        known_checks_sha,
        known_mergeability,
        known_mergeability_sha,
        mergeability_blank_since,
        known_decision,
        auto_merge,
        auto_mergeable) IS DISTINCT FROM (sqlc.narg('read_decision')::text,
        sqlc.narg('read_checks')::text,
        sqlc.narg('read_mergeability')::text,
        sqlc.narg('read_head_sha')::text,
        sqlc.narg('known_checks')::text,
        sqlc.narg('known_checks_sha')::text,
        sqlc.narg('known_mergeability')::text,
        sqlc.narg('known_mergeability_sha')::text,
        sqlc.narg('mergeability_blank_since')::timestamptz,
        sqlc.narg('known_decision')::text,
        sqlc.narg('auto_merge')::boolean,
        @auto_mergeable::boolean);

-- name: ListWatchedPullRequests :many
SELECT
    p.*
FROM
    pull_requests p
WHERE
    EXISTS (
        SELECT
            1
        FROM
            agent_pull_requests o
        WHERE
            o.pull_request_id = p.id)
    AND (sqlc.narg('keys')::text[] IS NULL
        OR p.id = ANY (sqlc.narg('keys')::uuid[]))
ORDER BY
    p.id;

-- name: ListPullRequestsPage :many
SELECT
    p.*
FROM
    pull_requests p
WHERE
    p.repository_url = @repository_url
    AND (@head_branch::text = ''
        OR p.head_branch = @head_branch::text)
    AND (sqlc.narg('cursor_id')::uuid IS NULL
        OR ((p.merged_at IS NOT NULL
                OR p.closed_at IS NOT NULL) > @cursor_finished::boolean)
        OR ((p.merged_at IS NOT NULL
                OR p.closed_at IS NOT NULL) = @cursor_finished::boolean
            AND (p.opened_at < @cursor_opened_at::timestamptz
                OR (p.opened_at = @cursor_opened_at::timestamptz
                    AND p.id < sqlc.narg('cursor_id')))))
ORDER BY
    (p.merged_at IS NOT NULL
        OR p.closed_at IS NOT NULL),
    p.opened_at DESC,
    p.id DESC
LIMIT @lim;

-- name: ListWatchedRepositories :many
SELECT
    *
FROM
    pull_request_syncs
WHERE
    sqlc.narg('keys')::text[] IS NULL
    OR repository_url = ANY (sqlc.narg('keys')::text[])
ORDER BY
    repository_url;

-- name: GetPullRequest :one
SELECT
    *
FROM
    pull_requests
WHERE
    id = @id;

-- name: ListPullRequestRecipients :many
SELECT
    a.id
FROM
    agents a
    JOIN pull_requests p ON p.repository_url = a.repository_url
        AND p.head_repository_url = a.repository_url
        AND p.head_branch = a.branch
    JOIN workflows w ON w.id = a.workflow_id
    LEFT JOIN workflow_provisioner_configs c ON c.id = w.provisioner_config_id
WHERE
    p.id = @id
    AND a.deleting_at IS NULL
    AND a.archived_at IS NULL
    AND a.parent_agent_id IS NULL
    AND a.branch <> ''
    AND (c.worktree_base_branch IS NULL
        OR a.branch <> c.worktree_base_branch)
ORDER BY
    a.id;
