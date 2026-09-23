-- name: EnsureTenant :exec
WITH wanted AS (
    SELECT
        @id::uuid AS id)
INSERT INTO tenants(
    id,
    name)
SELECT
    id,
    id::text
FROM
    wanted
ON CONFLICT (id)
    DO NOTHING;

-- name: ListOtherWeaveHosts :many
SELECT DISTINCT
    SPLIT_PART(application_name, '/', 2)::text AS host
FROM
    pg_catalog.pg_stat_activity
WHERE
    datname = CURRENT_DATABASE()
    AND application_name LIKE 'weave/%'
    AND SPLIT_PART(application_name, '/', 2) <> @host::text
ORDER BY
    host;
