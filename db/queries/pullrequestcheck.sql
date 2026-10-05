-- name: RecordStatusCheckReading :one
INSERT INTO pull_request_check_readings(
    pull_request_id,
    head_sha,
    blank,
    first_read_at,
    read_at)
VALUES (
    @pull_request_id,
    @head_sha,
    @blank,
    @read_at,
    @read_at)
ON CONFLICT (
    engine_id,
    pull_request_id,
    head_sha)
    DO UPDATE SET
        blank = excluded.blank,
        read_at = excluded.read_at
    RETURNING
        id;

-- name: UpsertStatusChecks :batchexec
INSERT INTO pull_request_checks(
    reading_id,
    provider_id,
    kind,
    source,
    name,
    state,
    conclusion,
    started_at,
    finished_at,
    required,
    url,
    run_id,
    run_event,
    run_started_at,
    job_id)
VALUES (
    @reading_id,
    @provider_id,
    @kind,
    @source,
    @name,
    @state,
    @conclusion,
    @started_at,
    @finished_at,
    @required,
    @url,
    @run_id,
    @run_event,
    @run_started_at,
    @job_id)
ON CONFLICT (
    engine_id,
    reading_id,
    provider_id)
    DO UPDATE SET
        kind = excluded.kind,
        source = excluded.source,
        name = excluded.name,
        state = excluded.state,
        conclusion = excluded.conclusion,
        started_at = excluded.started_at,
        finished_at = excluded.finished_at,
        required = excluded.required,
        url = excluded.url,
        run_id = excluded.run_id,
        run_event = excluded.run_event,
        run_started_at = excluded.run_started_at,
        job_id = excluded.job_id
    WHERE (
        pull_request_checks.kind, pull_request_checks.source, pull_request_checks.name, pull_request_checks.state, pull_request_checks.conclusion, pull_request_checks.started_at, pull_request_checks.finished_at, pull_request_checks.required, pull_request_checks.url, pull_request_checks.run_id, pull_request_checks.run_event, pull_request_checks.run_started_at, pull_request_checks.job_id) IS DISTINCT FROM (
        excluded.kind,
        excluded.source,
        excluded.name,
        excluded.state,
        excluded.conclusion,
        excluded.started_at,
        excluded.finished_at,
        excluded.required,
        excluded.url,
        excluded.run_id,
        excluded.run_event,
        excluded.run_started_at,
        excluded.job_id);

-- name: DeleteStatusChecksExcept :exec
DELETE FROM pull_request_checks
WHERE reading_id = @reading_id
    AND NOT (provider_id = ANY (@kept::text[]));

-- name: ListStatusCheckReadings :many
SELECT
    *
FROM
    pull_request_check_readings r
WHERE
    r.pull_request_id = @pull_request_id
ORDER BY
    r.first_read_at,
    r.id;

-- name: ListStatusChecksOfReadings :many
SELECT
    *
FROM
    pull_request_checks c
WHERE
    c.reading_id = ANY (@reading_ids::uuid[])
ORDER BY
    c.reading_id,
    c.kind,
    c.source,
    c.name,
    c.id;

-- name: ListHeadStatusCheckReadings :many
SELECT DISTINCT ON (r.pull_request_id)
    r.*
FROM
    pull_request_check_readings r
    JOIN pull_requests p ON p.id = r.pull_request_id
WHERE
    r.pull_request_id = ANY (@pull_request_ids::uuid[])
ORDER BY
    r.pull_request_id,
(r.head_sha = p.head_sha) DESC,
    r.read_at DESC,
    r.id DESC;

-- name: UpsertStatusCheckSteps :batchexec
INSERT INTO pull_request_check_steps(
    check_id,
    reading_id,
    number,
    name,
    state,
    conclusion,
    started_at,
    finished_at)
SELECT
    c.id,
    c.reading_id,
    @number,
    @name,
    @state,
    @conclusion,
    @started_at,
    @finished_at
FROM
    pull_request_checks c
WHERE
    c.reading_id = @reading_id
    AND c.provider_id = @provider_id
ON CONFLICT (engine_id,
    check_id,
    number)
    DO UPDATE SET
        name = excluded.name,
        state = excluded.state,
        conclusion = excluded.conclusion,
        started_at = excluded.started_at,
        finished_at = excluded.finished_at
    WHERE (
        pull_request_check_steps.name, pull_request_check_steps.state, pull_request_check_steps.conclusion, pull_request_check_steps.started_at, pull_request_check_steps.finished_at) IS DISTINCT FROM (
        excluded.name,
        excluded.state,
        excluded.conclusion,
        excluded.started_at,
        excluded.finished_at);

-- name: DeleteStatusCheckStepsExcept :exec
DELETE FROM pull_request_check_steps s USING pull_request_checks c
WHERE s.check_id = c.id
    AND c.reading_id = @reading_id
    AND c.provider_id = ANY (@complete::text[])
    AND NOT EXISTS (
        SELECT
            1
        FROM (
            SELECT
                UNNEST(@kept_provider_ids::text[]) AS provider_id,
                UNNEST(@kept_numbers::integer[]) AS number) k
        WHERE
            k.provider_id = c.provider_id
            AND k.number = s.number);

-- name: ListStatusCheckStepsOfReadings :many
SELECT
    *
FROM
    pull_request_check_steps s
WHERE
    s.reading_id = ANY (@reading_ids::uuid[])
ORDER BY
    s.check_id,
    s.number;

-- name: ListCheckFailureNotes :many
SELECT
    c.provider_id,
    n.finished_at,
    n.noted_at
FROM
    pull_request_check_failure_notes n
    JOIN pull_request_checks c ON c.id = n.check_id
    JOIN pull_request_check_readings r ON r.id = c.reading_id
WHERE
    n.agent_id = @agent_id
    AND r.pull_request_id = @pull_request_id
    AND r.head_sha = @head_sha;

-- name: NoteCheckFailure :batchexec
INSERT INTO pull_request_check_failure_notes(
    agent_id,
    check_id,
    finished_at)
SELECT
    @agent_id,
    c.id,
    @finished_at
FROM
    pull_request_checks c
    JOIN pull_request_check_readings r ON r.id = c.reading_id
WHERE
    r.pull_request_id = @pull_request_id
    AND r.head_sha = @head_sha
    AND c.provider_id = @provider_id
ON CONFLICT
    DO NOTHING;

-- name: RecordCheckLog :one
INSERT INTO pull_request_check_logs(
    check_id,
    fetched_at,
    outcome,
    text,
    line_count,
    error_count)
VALUES (
    @check_id,
    @fetched_at,
    @outcome,
    @text,
    @line_count,
    @error_count)
ON CONFLICT (
    engine_id,
    check_id)
    DO UPDATE SET
        fetched_at = excluded.fetched_at,
        outcome = excluded.outcome,
        text = excluded.text,
        line_count = excluded.line_count,
        error_count = excluded.error_count
    RETURNING
        id;

-- name: DeleteCheckLogSteps :exec
DELETE FROM pull_request_check_log_steps
WHERE log_id = @log_id;

-- name: InsertCheckLogSteps :batchexec
INSERT INTO pull_request_check_log_steps(
    log_id,
    number,
    name,
    conclusion,
    first_line,
    line_count,
    error_count)
VALUES (
    @log_id,
    @number,
    @name,
    @conclusion,
    @first_line,
    @line_count,
    @error_count);

-- name: ListCheckLogsOfReadings :many
SELECT
    l.check_id,
    l.fetched_at,
    l.outcome,
    l.line_count,
    l.error_count
FROM
    pull_request_check_logs l
    JOIN pull_request_checks c ON c.id = l.check_id
WHERE
    c.reading_id = ANY (@reading_ids::uuid[])
ORDER BY
    l.check_id;

-- name: ListCheckLogStepsOfReadings :many
SELECT
    l.check_id,
    s.number,
    s.name,
    s.conclusion,
    s.first_line,
    s.line_count,
    s.error_count
FROM
    pull_request_check_log_steps s
    JOIN pull_request_check_logs l ON l.id = s.log_id
    JOIN pull_request_checks c ON c.id = l.check_id
WHERE
    c.reading_id = ANY (@reading_ids::uuid[])
ORDER BY
    l.check_id,
    s.number;

-- name: GetCheckLogText :one
SELECT
    l.text,
    l.line_count
FROM
    pull_request_check_logs l
WHERE
    l.check_id = @check_id
    AND l.outcome = 'fetched';

-- name: ListCheckLogTexts :many
SELECT
    l.check_id,
    l.text
FROM
    pull_request_check_logs l
WHERE
    l.check_id = ANY (@check_ids::uuid[])
    AND l.outcome = 'fetched'
ORDER BY
    l.check_id;

-- name: ListCheckLogStepsOfChecks :many
SELECT
    l.check_id,
    s.number,
    s.first_line,
    s.line_count
FROM
    pull_request_check_log_steps s
    JOIN pull_request_check_logs l ON l.id = s.log_id
WHERE
    l.check_id = ANY (@check_ids::uuid[])
ORDER BY
    l.check_id,
    s.first_line;

-- name: GetCheckForLogFetch :one
SELECT
    c.id,
    c.state,
    c.job_id,
    c.finished_at,
    p.host,
    p.repository_url
FROM
    pull_request_checks c
    JOIN pull_request_check_readings r ON r.id = c.reading_id
    JOIN pull_requests p ON p.id = r.pull_request_id
WHERE
    c.id = @check_id;

-- name: ListStepsOfCheck :many
SELECT
    s.number,
    s.name,
    s.conclusion,
    s.started_at
FROM
    pull_request_check_steps s
WHERE
    s.check_id = @check_id
ORDER BY
    s.number;

-- name: ListHeadChecksWantingLogs :many
SELECT
    c.id
FROM
    pull_request_checks c
    JOIN pull_request_check_readings r ON r.id = c.reading_id
    JOIN pull_requests p ON p.id = r.pull_request_id
WHERE
    r.pull_request_id = @pull_request_id
    AND r.head_sha = p.head_sha
    AND c.state = 'completed'
    AND c.job_id IS NOT NULL
    AND NOT EXISTS (
        SELECT
            1
        FROM
            pull_request_check_logs l
        WHERE
            l.check_id = c.id)
ORDER BY
    c.id;
