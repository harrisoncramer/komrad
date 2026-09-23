-- name: GetCurrentEngine :one
SELECT
    id,
    name
FROM
    tenants
WHERE
    id = current_tenant();
