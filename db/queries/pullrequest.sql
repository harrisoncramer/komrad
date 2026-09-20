-- name: UpsertPullRequest :one
WITH written AS (
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
    TRUE AS changed
FROM
    written
UNION ALL
SELECT
    pull_requests.id,
    FALSE AS inserted,
    FALSE AS changed
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

-- name: ListPullRequestsOnBranch :many
SELECT
    *
FROM
    pull_requests
WHERE
    repository_url = @repository_url
    AND head_branch = @head_branch
ORDER BY
    opened_at DESC;

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
    a.archived_at IS NULL
    AND a.repository_url IS NOT NULL
    AND a.branch IS NOT NULL
    AND a.branch <> ''
    AND (c.worktree_base_branch IS NULL
        OR c.worktree_base_branch = ''
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
    o.last_seen_at
FROM
    agent_pull_requests o
WHERE
    o.agent_id = @agent_id
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

-- name: SetBranchScanNextPage :exec
INSERT INTO branch_scans(
    repository_url,
    branch,
    next_page)
VALUES (
    @repository_url,
    @branch,
    @next_page)
ON CONFLICT (
    repository_url,
    branch)
    DO UPDATE SET
        next_page = EXCLUDED.next_page;

-- name: CompleteBranchScan :exec
INSERT INTO branch_scans(
    repository_url,
    branch,
    next_page,
    completed_at)
VALUES (
    @repository_url,
    @branch,
    @next_page,
    NOW())
ON CONFLICT (
    repository_url,
    branch)
    DO UPDATE SET
        next_page = EXCLUDED.next_page,
        completed_at = NOW();

-- name: EnsurePullRequestSync :exec
INSERT INTO pull_request_syncs(
    repository_url)
VALUES (
    @repository_url)
ON CONFLICT (
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
    sweep_next_page = 1,
    sweep_newest = @sweep_newest,
    sweep_etag = @sweep_etag,
    validated_at = COALESCE(sqlc.narg('validated_at'), validated_at)
WHERE
    repository_url = @repository_url;

-- name: AdvancePullRequestSweep :exec
UPDATE
    pull_request_syncs
SET
    sweep_next_page = @sweep_next_page
WHERE
    repository_url = @repository_url;

-- name: CompletePullRequestSweep :exec
UPDATE
    pull_request_syncs
SET
    updated_through = GREATEST(updated_through, sweep_newest),
    etag = COALESCE(sweep_etag, ''),
    sweep_next_page = NULL,
    sweep_newest = NULL,
    sweep_etag = NULL
WHERE
    repository_url = @repository_url;

-- name: ListStaleDecisions :many
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
    AND (p.decision_read_at IS NULL
        OR p.decision_read_at < p.changed_at)
    AND EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.archived_at IS NULL
            AND a.repository_url = p.repository_url
            AND a.branch = p.head_branch)
ORDER BY
    p.changed_at DESC;

-- name: SetReviewDecision :exec
UPDATE
    pull_requests
SET
    review_decision = sqlc.narg('review_decision'),
    decision_read_at = NOW()
WHERE
    id = @id;

-- name: ListCurrentPullRequests :many
SELECT DISTINCT ON (a.id)
    a.id AS agent_id,
    sqlc.embed(p)
FROM
    agents a
    JOIN pull_requests p ON p.repository_url = a.repository_url
        AND p.head_repository_url = a.repository_url
        AND p.head_branch = a.branch
    JOIN workflows w ON w.id = a.workflow_id
    LEFT JOIN workflow_provisioner_configs c ON c.id = w.provisioner_config_id
WHERE
    a.archived_at IS NULL
    AND (c.worktree_base_branch IS NULL
        OR c.worktree_base_branch = ''
        OR a.branch <> c.worktree_base_branch)
ORDER BY
    a.id,
    COALESCE(p.merged_at, p.closed_at) IS NOT NULL,
    p.opened_at DESC;

-- name: ListReadRepositories :many
SELECT
    repository_url
FROM
    pull_request_syncs
WHERE
    read_at IS NOT NULL
ORDER BY
    repository_url;
