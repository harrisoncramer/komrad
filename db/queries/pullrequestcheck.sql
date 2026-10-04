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

-- name: UpsertStatusChecks :exec
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
SELECT
    @reading_id,
    c.provider_id,
    c.kind,
    c.source,
    c.name,
    c.state,
    c.conclusion,
    c.started_at,
    c.finished_at,
    c.required,
    c.url
FROM
    JSONB_TO_RECORDSET(@checks::jsonb) AS c(provider_id text,
        kind text,
        source text,
        name text,
        state text,
        conclusion text,
        started_at timestamptz,
        finished_at timestamptz,
        required boolean,
        url text)
ON CONFLICT (engine_id,
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

-- name: ListLatestStatusCheckReadings :many
SELECT DISTINCT ON (r.pull_request_id)
    *
FROM
    pull_request_check_readings r
WHERE
    r.pull_request_id = ANY (@pull_request_ids::uuid[])
ORDER BY
    r.pull_request_id,
    r.read_at DESC,
    r.id DESC;

-- name: UpsertStatusCheckSteps :exec
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
    s.number,
    s.name,
    s.state,
    s.conclusion,
    s.started_at,
    s.finished_at
FROM
    JSONB_TO_RECORDSET(@steps::jsonb) AS s(provider_id text,
        number integer,
        name text,
        state text,
        conclusion text,
        started_at timestamptz,
        finished_at timestamptz)
    JOIN pull_request_checks c ON c.reading_id = @reading_id
        AND c.provider_id = s.provider_id
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
        FROM
            JSONB_TO_RECORDSET(@kept::jsonb) AS k(provider_id text,
                number integer)
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
