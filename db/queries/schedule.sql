-- name: ListSchedulesPage :many
SELECT
    sqlc.embed(s)
FROM
    schedules s
WHERE
    s.is_deleted = FALSE
    AND (sqlc.narg ('cursor_updated_at')::timestamptz IS NULL
        OR s.updated_at < sqlc.narg ('cursor_updated_at')
        OR (s.updated_at = sqlc.narg ('cursor_updated_at')
            AND s.id < sqlc.narg ('cursor_id')))
ORDER BY
    s.updated_at DESC,
    s.id DESC
LIMIT @lim;

-- name: ListSchedules :many
SELECT
    sqlc.embed(s)
FROM
    schedules s
WHERE
    s.is_deleted = FALSE
ORDER BY
    s.updated_at DESC;

-- name: GetSchedule :one
SELECT
    sqlc.embed(s)
FROM
    schedules s
WHERE
    s.id = @id
    AND s.is_deleted = FALSE
LIMIT 1;

-- name: InsertSchedule :one
INSERT INTO schedules (name, cron, timezone, enabled, prompt, workflow_id)
    VALUES (@name, @cron, @timezone, @enabled, @prompt, @workflow_id)
RETURNING
    *;

-- name: UpdateSchedule :exec
UPDATE
    schedules
SET
    name = @name,
    cron = @cron,
    timezone = @timezone,
    enabled = @enabled,
    prompt = @prompt,
    workflow_id = @workflow_id
WHERE
    id = @id
    AND is_deleted = FALSE;

-- name: SetScheduleEnabled :exec
UPDATE
    schedules
SET
    enabled = @enabled
WHERE
    id = @id
    AND is_deleted = FALSE;

-- name: MarkScheduleDeleted :exec
UPDATE
    schedules
SET
    is_deleted = TRUE
WHERE
    id = @id;

-- name: CountLiveWorkflowSchedules :one
SELECT
    count(*)
FROM
    schedules
WHERE
    workflow_id = @workflow_id
    AND is_deleted = FALSE;
