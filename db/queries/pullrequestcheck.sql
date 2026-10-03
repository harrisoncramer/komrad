-- name: RecordCheckReading :one
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

-- name: UpsertCheck :exec
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
    url)
VALUES (
    @reading_id,
    @provider_id,
    @kind,
    @source,
    @name,
    @state,
    @conclusion,
    sqlc.narg('started_at'),
    sqlc.narg('finished_at'),
    @required,
    @url)
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
        url = excluded.url
    WHERE (
        pull_request_checks.kind, pull_request_checks.source, pull_request_checks.name, pull_request_checks.state, pull_request_checks.conclusion, pull_request_checks.started_at, pull_request_checks.finished_at, pull_request_checks.required, pull_request_checks.url) IS DISTINCT FROM (
        excluded.kind,
        excluded.source,
        excluded.name,
        excluded.state,
        excluded.conclusion,
        excluded.started_at,
        excluded.finished_at,
        excluded.required,
        excluded.url);

-- name: DeleteChecksExcept :exec
DELETE FROM pull_request_checks
WHERE reading_id = @reading_id
    AND NOT (provider_id = ANY (@kept::text[]));

-- name: ListCheckReadings :many
SELECT
    *
FROM
    pull_request_check_readings r
WHERE
    r.pull_request_id = @pull_request_id
ORDER BY
    r.first_read_at,
    r.id;

-- name: ListChecksOfReadings :many
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
