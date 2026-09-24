-- name: ListSchedulesPage :many
SELECT
    sqlc.embed(s)
FROM
    schedules s
WHERE
    s.archived_at IS NULL
    AND (sqlc.narg('cursor_id')::uuid IS NULL
        OR s.id < sqlc.narg('cursor_id'))
ORDER BY
    s.id DESC
LIMIT @lim;

-- name: ListWatchedSchedules :many
SELECT
    sqlc.embed(s)
FROM
    schedules s
WHERE
    s.archived_at IS NULL
    AND (sqlc.narg('keys')::text[] IS NULL
        OR s.id = ANY (sqlc.narg('keys')::uuid[]))
ORDER BY
    s.id DESC;

-- name: GetSchedule :one
SELECT
    sqlc.embed(s)
FROM
    schedules s
WHERE
    s.id = @id
    AND s.archived_at IS NULL
LIMIT 1;

-- name: InsertSchedule :one
INSERT INTO schedules(
    name,
    cron,
    timezone,
    enabled,
    hide_agents,
    prompt,
    workflow_id)
VALUES (
    @name,
    @cron,
    @timezone,
    @enabled,
    @hide_agents,
    @prompt,
    @workflow_id)
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
    hide_agents = @hide_agents,
    prompt = @prompt,
    workflow_id = @workflow_id
WHERE
    id = @id
    AND archived_at IS NULL;

-- name: SetScheduleEnabled :exec
UPDATE
    schedules
SET
    enabled = @enabled
WHERE
    id = @id
    AND archived_at IS NULL;

-- name: MarkScheduleArchived :exec
UPDATE
    schedules
SET
    archived_at = NOW()
WHERE
    id = @id
    AND archived_at IS NULL;

-- name: CountLiveWorkflowSchedules :one
SELECT
    COUNT(*)
FROM
    schedules
WHERE
    workflow_id = @workflow_id
    AND archived_at IS NULL;

-- name: ListWorkflowSchedules :many
SELECT
    sqlc.embed(s)
FROM
    schedules s
WHERE
    s.workflow_id = @workflow_id
ORDER BY
    s.id DESC;

-- name: DeleteSchedule :exec
DELETE FROM schedules
WHERE id = @id;
