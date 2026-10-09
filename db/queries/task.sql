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
