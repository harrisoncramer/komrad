-- name: ListSchedulesPage :many
SELECT
    sqlc.embed(s),
    sc.cwd,
    sc.flags
FROM
    schedules s
    JOIN schedule_configs sc ON sc.id = s.schedule_config_id
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
    sqlc.embed(s),
    sc.cwd,
    sc.flags
FROM
    schedules s
    JOIN schedule_configs sc ON sc.id = s.schedule_config_id
WHERE
    s.is_deleted = FALSE
ORDER BY
    s.updated_at DESC;

-- name: GetSchedule :one
SELECT
    sqlc.embed(s),
    sc.cwd,
    sc.flags
FROM
    schedules s
    JOIN schedule_configs sc ON sc.id = s.schedule_config_id
WHERE
    s.id = @id
    AND s.is_deleted = FALSE
LIMIT 1;

-- name: InsertScheduleConfig :one
INSERT INTO schedule_configs (cwd, flags)
    VALUES (@cwd, @flags)
RETURNING
    id;

-- name: InsertSchedule :one
INSERT INTO schedules (name, cron, timezone, enabled, prompt, workflow_id, schedule_config_id)
    VALUES (@name, @cron, @timezone, @enabled, @prompt, sqlc.narg ('workflow_id'), @schedule_config_id)
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
    workflow_id = sqlc.narg ('workflow_id')
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

-- name: UpdateScheduleConfig :exec
UPDATE
    schedule_configs
SET
    cwd = @cwd,
    flags = @flags
WHERE
    id = (
        SELECT
            schedule_config_id
        FROM
            schedules
        WHERE
            schedules.id = @schedule_id);

-- name: MarkScheduleDeleted :exec
UPDATE
    schedules
SET
    is_deleted = TRUE
WHERE
    id = @id;
