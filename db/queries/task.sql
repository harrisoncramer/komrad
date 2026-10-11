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

-- name: GetTaskByAgentID :one
SELECT
    *
FROM
    tasks
WHERE
    id =(
        SELECT
            task_id
        FROM
            agents
        WHERE
            agents.id = sqlc.arg('agent_id'));

-- name: ListTasks :many
SELECT
    *
FROM
    tasks
WHERE
    workflow_id = sqlc.arg('workflow_id')
    AND deleting_at IS NULL
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

-- name: ArchiveIdleTasks :many
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
            AND a.archived_at IS NULL)
RETURNING
    t.*;

-- name: RestoreTasks :exec
UPDATE
    tasks
SET
    archived_at = NULL,
    is_hidden = FALSE
WHERE
    id = ANY (sqlc.arg('ids')::uuid[])
    AND archived_at IS NOT NULL;

-- name: SetTaskHidden :execrows
UPDATE
    tasks
SET
    is_hidden = sqlc.arg('hidden')
WHERE
    id = sqlc.arg('id')
    AND archived_at IS NULL
    AND deleting_at IS NULL;

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
WITH watched AS (
    SELECT
        t.id
    FROM
        tasks t
    WHERE
        t.deleting_at IS NULL
        AND (sqlc.narg('keys')::text[] IS NULL
            OR t.id = ANY (sqlc.narg('keys')::uuid[]))
),
written AS (
    SELECT
        d.task_id,
        COUNT(*)::integer AS documents,
        MAX(d.updated_at)::timestamptz AS documents_updated_at
    FROM
        watched w
        JOIN documents d ON d.task_id = w.id
    GROUP BY
        d.task_id
)
SELECT
    sqlc.embed(t),
    EXISTS (
        SELECT
        FROM
            agent_runs r
        WHERE
            r.task_id = t.id
            AND r.failure <> ''
            AND r.viewed_at IS NULL)::boolean AS failed_run,
    COALESCE(d.documents, 0)::integer AS documents,
    d.documents_updated_at
FROM
    watched w
    JOIN tasks t ON t.id = w.id
    LEFT JOIN written d ON d.task_id = t.id
ORDER BY
    t.id DESC;

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

-- name: UpdateTask :one
UPDATE
    tasks
SET
    slug = COALESCE(sqlc.narg('slug'), slug),
    title = COALESCE(sqlc.narg('title'), title),
    goal = COALESCE(sqlc.narg('goal'), goal),
    notes = COALESCE(sqlc.narg('notes'), notes),
    notes_updated_at = CASE WHEN sqlc.narg('notes')::text IS NULL THEN
        notes_updated_at
    ELSE
        NOW()
    END
WHERE
    id = sqlc.arg('id')
    AND deleting_at IS NULL
    AND (sqlc.narg('notes')::text IS NULL
        OR notes_updated_at IS NOT DISTINCT FROM sqlc.narg('read_notes_updated_at')::timestamptz)
RETURNING
    *;

-- name: MarkTasksDeleting :exec
UPDATE
    tasks
SET
    deleting_at = NOW(),
    failure_reason = ''
WHERE
    id = ANY (sqlc.arg('ids')::uuid[])
    AND deleting_at IS NULL;

-- name: MarkWorkflowTasksDeleting :exec
UPDATE
    tasks
SET
    deleting_at = NOW(),
    failure_reason = ''
WHERE
    workflow_id = sqlc.arg('workflow_id')
    AND deleting_at IS NULL;

-- name: ClaimEmptyDeletingTask :execrows
UPDATE
    tasks t
SET
    cleaning_at = NOW()
WHERE
    t.id = sqlc.arg('id')
    AND t.deleting_at IS NOT NULL
    AND t.cleaning_at IS NULL
    AND NOT EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.task_id = t.id);

-- name: ClearTaskDeleting :exec
UPDATE
    tasks
SET
    deleting_at = NULL,
    cleaning_at = NULL
WHERE
    id = sqlc.arg('id');

-- name: FailTaskCleanup :exec
UPDATE
    tasks
SET
    deleting_at = NULL,
    cleaning_at = NULL,
    failure_reason = sqlc.arg('failure_reason')
WHERE
    id = sqlc.arg('id');

-- name: ClearAbandonedTasksDeleting :exec
UPDATE
    tasks t
SET
    deleting_at = NULL,
    cleaning_at = NULL
WHERE
    t.deleting_at IS NOT NULL
    AND NOT EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.task_id = t.id
            AND a.deleting_at IS NOT NULL);

-- name: ListWorkflowTaskIDs :many
SELECT
    id
FROM
    tasks
WHERE
    workflow_id = sqlc.arg('workflow_id')
ORDER BY
    id;

-- name: DeleteTask :exec
DELETE FROM tasks
WHERE id = sqlc.arg('id');

-- name: LockUndeletedTask :one
SELECT
    id
FROM
    tasks
WHERE
    id = sqlc.arg('id')
    AND deleting_at IS NULL
FOR NO KEY UPDATE;

-- name: ListUndeletedTaskSlugs :many
SELECT
    id,
    slug
FROM
    tasks
WHERE
    deleting_at IS NULL;

-- name: GetUndeletedTaskForUpdate :one
SELECT
    *
FROM
    tasks
WHERE
    id = sqlc.arg('id')
    AND deleting_at IS NULL
FOR UPDATE;

-- name: LockTasks :exec
SELECT
    id
FROM
    tasks
WHERE
    id = ANY (sqlc.arg('ids')::uuid[])
ORDER BY
    id
FOR UPDATE;

-- name: ArchiveIdleTaskBesides :exec
UPDATE
    tasks t
SET
    archived_at = NOW()
WHERE
    t.id = sqlc.arg('id')
    AND t.archived_at IS NULL
    AND NOT EXISTS (
        SELECT
            1
        FROM
            agents a
        WHERE
            a.task_id = t.id
            AND a.archived_at IS NULL
            AND a.id <> sqlc.arg('agent_id'));

-- name: ListTaskSummaries :many
SELECT
    id,
    workflow_id,
    slug,
    adopted_branch,
    archived_at,
    deleting_at
FROM
    tasks;
