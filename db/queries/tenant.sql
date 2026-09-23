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
