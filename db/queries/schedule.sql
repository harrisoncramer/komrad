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

-- name: StartScheduleRun :exec
INSERT INTO schedule_runs(
    schedule_id,
    scheduled_for,
    state,
    attempt,
    max_attempts)
VALUES (
    @schedule_id,
    @scheduled_for,
    'running',
    @attempt,
    @max_attempts)
ON CONFLICT (
    tenant_id,
    schedule_id,
    scheduled_for)
    DO UPDATE SET
        state = 'running',
        attempt = EXCLUDED.attempt,
        max_attempts = EXCLUDED.max_attempts,
        finalized_at = NULL;

-- name: FinishScheduleRun :exec
UPDATE
    schedule_runs
SET
    state = @state::text,
    message = @message,
    finalized_at = CASE WHEN @state::text IN ('completed', 'cancelled', 'discarded') THEN
        NOW()
    END
WHERE
    schedule_id = @schedule_id
    AND scheduled_for = @scheduled_for;

-- name: PruneScheduleRuns :exec
DELETE FROM schedule_runs gone
WHERE gone.schedule_id = @schedule_id
    AND gone.id NOT IN (
        SELECT
            kept.id
        FROM
            schedule_runs kept
        WHERE
            kept.schedule_id = @schedule_id
        ORDER BY
            kept.scheduled_for DESC
        LIMIT @keep);

-- name: ListScheduleRuns :many
SELECT
    sqlc.embed(r)
FROM
    schedule_runs r
WHERE
    r.schedule_id = @schedule_id
ORDER BY
    r.scheduled_for DESC
LIMIT @lim;

-- name: ListRecentScheduleRuns :many
SELECT
    sqlc.embed(r)
FROM
    schedule_runs r
WHERE
    r.schedule_id = ANY (@schedule_ids::uuid[])
    AND r.id IN (
        SELECT
            recent.id
        FROM
            UNNEST(@schedule_ids::uuid[]) AS s(id)
            CROSS JOIN LATERAL (
                SELECT
                    inner_run.id
                FROM
                    schedule_runs inner_run
                WHERE
                    inner_run.schedule_id = s.id
                ORDER BY
                    inner_run.scheduled_for DESC
                LIMIT @per_schedule) recent)
ORDER BY
    r.schedule_id,
    r.scheduled_for DESC;
