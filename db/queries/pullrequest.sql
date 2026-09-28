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

-- name: ListUnscannedBranches :many
SELECT DISTINCT
    a.branch
FROM
    agents a
    LEFT JOIN branch_scans b ON b.repository_url = a.repository_url
        AND b.branch = a.branch
WHERE
    a.archived_at IS NULL
    AND a.repository_url = @repository_url
    AND a.branch IS NOT NULL
    AND a.branch <> ''
    AND b.completed_at IS NULL
ORDER BY
    a.branch;

-- name: GetBranchScan :one
SELECT
    *
FROM
    branch_scans
WHERE
    repository_url = @repository_url
    AND branch = @branch;

-- name: SetBranchScanCursor :exec
INSERT INTO branch_scans(
    repository_url,
    branch,
    next_cursor)
VALUES (
    @repository_url,
    @branch,
    @next_cursor)
ON CONFLICT (
    engine_id,
    repository_url,
    branch)
    DO UPDATE SET
        next_cursor = EXCLUDED.next_cursor;

-- name: CompleteBranchScan :exec
INSERT INTO branch_scans(
    repository_url,
    branch,
    next_cursor,
    completed_at)
VALUES (
    @repository_url,
    @branch,
    @next_cursor,
    NOW())
ON CONFLICT (
    engine_id,
    repository_url,
    branch)
    DO UPDATE SET
        next_cursor = EXCLUDED.next_cursor,
        completed_at = NOW();

-- name: EnsurePullRequestSync :exec
INSERT INTO pull_request_syncs(
    repository_url)
VALUES (
    @repository_url)
ON CONFLICT (
    engine_id,
    repository_url)
    DO NOTHING;

-- name: GetPullRequestSync :one
SELECT
    *
FROM
    pull_request_syncs
WHERE
    repository_url = @repository_url;

-- name: ResetPullRequestSyncCalls :exec
UPDATE
    pull_request_syncs
SET
    calls = 0
WHERE
    repository_url = @repository_url;

-- name: RecordPullRequestSyncRequest :exec
UPDATE
    pull_request_syncs
SET
    read_at = NOW(),
    calls = calls + 1
WHERE
    repository_url = @repository_url;

-- name: StartPullRequestSweep :exec
UPDATE
    pull_request_syncs
SET
    sweep_cursor = '',
    sweep_newest = @sweep_newest,
    listed_at = @listed_at
WHERE
    repository_url = @repository_url;

-- name: AdvancePullRequestSweep :exec
UPDATE
    pull_request_syncs
SET
    sweep_cursor = @sweep_cursor
WHERE
    repository_url = @repository_url;

-- name: CompletePullRequestSweep :exec
UPDATE
    pull_request_syncs
SET
    updated_through = GREATEST(updated_through, sweep_newest),
    sweep_cursor = NULL,
    sweep_newest = NULL
WHERE
    repository_url = @repository_url;

-- name: ResetPullRequestSweep :exec
UPDATE
    pull_request_syncs
SET
    updated_through = NULL,
    sweep_cursor = NULL,
    sweep_newest = NULL
WHERE
    repository_url = ANY (@repository_urls::text[]);

-- name: AdvancePullRequestSyncMark :exec
UPDATE
    pull_request_syncs
SET
    updated_through = GREATEST(updated_through, @updated_through::timestamptz)
WHERE
    repository_url = @repository_url;

-- name: MarkPullRequestSyncListed :exec
UPDATE
    pull_request_syncs
SET
    listed_at = @listed_at
WHERE
    repository_url = @repository_url;

-- name: ListStaleStandings :many
SELECT
    p.*
FROM
    pull_requests p
WHERE
    p.repository_url = @repository_url
    AND p.head_repository_url = p.repository_url
    AND p.merged_at IS NULL
    AND p.closed_at IS NULL
    AND NOT p.draft
    AND (p.decision_changed_at IS NULL
        OR p.decision_changed_at < p.changed_at
        OR p.checks_sha IS DISTINCT FROM p.head_sha
        OR (p.checks = 'pending'
            AND p.checks_read_at < @pending_before::timestamptz)
        OR ((p.checks IS NULL
                OR p.mergeability IS NULL)
            AND p.changed_at > @changed_since::timestamptz
            AND p.checks_read_at < @pending_before::timestamptz)
        OR (p.checks_read_at < @settled_before::timestamptz))
    AND EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.repository_url = p.repository_url
            AND a.branch = p.head_branch)
ORDER BY
    p.changed_at DESC;

-- name: SetStanding :one
WITH prior AS (
    SELECT
        held.id,
        held.last_known_checks,
        held.last_known_mergeability,
        held.last_known_decision
    FROM
        pull_requests held
    WHERE
        held.id = @id
    FOR UPDATE)
UPDATE
    pull_requests p
SET
    review_decision = sqlc.narg('review_decision'),
    decision_changed_at = p.changed_at,
    checks = sqlc.narg('checks'),
    mergeability = sqlc.narg('mergeability'),
    checks_sha = @checks_sha::text,
    checks_read_at = @checks_read_at::timestamptz,
    last_known_checks = COALESCE(sqlc.narg('checks'), p.last_known_checks),
    last_known_mergeability = COALESCE(sqlc.narg('mergeability'), p.last_known_mergeability),
    last_known_decision = COALESCE(sqlc.narg('review_decision'), p.last_known_decision)
FROM
    prior
WHERE
    p.id = prior.id
RETURNING
    prior.last_known_checks AS old_checks,
    prior.last_known_mergeability AS old_mergeability,
    prior.last_known_decision AS old_decision,
    p.last_known_checks AS known_checks,
    p.last_known_mergeability AS known_mergeability,
    p.last_known_decision AS known_decision;

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
