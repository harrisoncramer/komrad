-- name: CreateTask :one
INSERT INTO tasks(
    workflow_id,
    slug,
    title,
    goal)
VALUES (
    sqlc.arg('workflow_id'),
    sqlc.arg('slug'),
    sqlc.arg('title'),
    sqlc.arg('goal'))
RETURNING
    *;

-- name: GetTaskByID :one
SELECT
    *
FROM
    tasks
WHERE
    id = sqlc.arg('id');

-- name: GetTaskBySlug :one
SELECT
    *
FROM
    tasks
WHERE
    slug = sqlc.arg('slug');

-- name: ListTasks :many
SELECT
    *
FROM
    tasks
WHERE
    workflow_id = sqlc.arg('workflow_id')
    AND (sqlc.arg('archived')::boolean) =(archived_at IS NOT NULL)
ORDER BY
    id DESC;

-- name: ArchiveTask :execrows
UPDATE
    tasks
SET
    archived_at = NOW()
WHERE
    id = sqlc.arg('id')
    AND archived_at IS NULL;

-- name: ArchiveIdleTasks :exec
UPDATE
    tasks t
SET
    archived_at = NOW()
WHERE
    t.id = ANY (sqlc.arg('ids')::uuid[])
    AND t.archived_at IS NULL
    AND NOT EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.task_id = t.id
            AND a.archived_at IS NULL);

-- name: RestoreTasks :exec
UPDATE
    tasks
SET
    archived_at = NULL
WHERE
    id = ANY (sqlc.arg('ids')::uuid[])
    AND archived_at IS NOT NULL;

-- name: IsTaskSlugTaken :one
SELECT
    EXISTS (
        SELECT
            1
        FROM
            tasks
        WHERE
            slug = sqlc.arg('slug'))::boolean;

-- name: LockTaskSlugs :exec
SELECT
    PG_ADVISORY_XACT_LOCK(HASHTEXTEXTENDED(COALESCE(current_engine()::text, ''), 7240117));

-- name: ListWatchedTasks :many
SELECT
    *
FROM
    tasks
WHERE (sqlc.narg('keys')::text[] IS NULL
    OR id = ANY (sqlc.narg('keys')::uuid[]))
ORDER BY
    id DESC;

-- name: ClaimTaskCheckout :execrows
UPDATE
    tasks
SET
    adopted_branch = sqlc.narg('adopted_branch'),
    branch = sqlc.narg('adopted_branch'),
    repository_url = sqlc.narg('repository_url'),
    worktree_removed = FALSE
WHERE
    id = sqlc.arg('id')
    AND worktree_removed IS NULL;

-- name: ClaimTaskCheckoutAgain :execrows
UPDATE
    tasks
SET
    branch = adopted_branch,
    worktree_removed = FALSE
WHERE
    id = sqlc.arg('id')
    AND worktree_removed IS NULL;

-- name: ReleaseTaskCheckout :exec
UPDATE
    tasks
SET
    cwd = NULL,
    branch = NULL,
    head_sha = NULL,
    SEQUENCE =
        NULL,
        lines_added = NULL,
        lines_removed = NULL,
        moved_since_round = NULL,
        last_rebase = NULL,
        worktree_removed = NULL
    WHERE
        id = sqlc.arg('id');

-- name: SetTaskCheckoutDir :exec
UPDATE
    tasks
SET
    cwd = sqlc.arg('cwd')
WHERE
    id = sqlc.arg('id');

-- name: SetTaskHead :exec
UPDATE
    tasks
SET
    branch = sqlc.narg('branch'),
    repository_url = sqlc.narg('repository_url'),
    head_sha = sqlc.narg('head_sha'),
    worktree_removed = sqlc.arg('worktree_removed'),
    "sequence" = sqlc.arg('sequence')
WHERE
    id = sqlc.arg('id');

-- name: SetTaskSequence :exec
UPDATE
    tasks
SET
    "sequence" = sqlc.arg('sequence')
WHERE
    id = sqlc.arg('id');

-- name: SetTaskMovedSinceRound :exec
UPDATE
    tasks
SET
    moved_since_round = sqlc.arg('moved_since_round')
WHERE
    id = sqlc.arg('id');

-- name: SetTaskDiffStat :exec
UPDATE
    tasks
SET
    lines_added = sqlc.narg('lines_added'),
    lines_removed = sqlc.narg('lines_removed')
WHERE
    id = sqlc.arg('id');

-- name: SetTaskLastRebase :exec
UPDATE
    tasks
SET
    last_rebase = sqlc.arg('last_rebase')
WHERE
    id = sqlc.arg('id');

-- name: GetTaskLastRebase :one
SELECT
    last_rebase
FROM
    tasks
WHERE
    id = sqlc.arg('id');

-- name: ListTaskCheckouts :many
SELECT
    t.*
FROM
    tasks t
WHERE
    t.archived_at IS NULL
    AND t.cwd IS NOT NULL
    AND t.cwd <> ''
    AND EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.task_id = t.id
            AND a.archived_at IS NULL)
ORDER BY
    t.id;

-- name: ListTasksByID :many
SELECT
    *
FROM
    tasks
WHERE
    id = ANY (sqlc.arg('ids')::uuid[])
ORDER BY
    id;

-- name: MarkTaskCheckoutsRemoved :exec
UPDATE
    tasks t
SET
    worktree_removed = TRUE
FROM
    agents a
WHERE
    a.id = ANY (sqlc.arg('agent_ids')::uuid[])
    AND a.task_id = t.id
    AND t.cwd = a.cwd;
