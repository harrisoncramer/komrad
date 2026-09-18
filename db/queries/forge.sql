-- name: CreateForgeConnection :one
INSERT INTO forge_connections(
    kind,
    host,
    api_url,
    method,
    login,
    verified_at)
VALUES (
    @kind,
    @host,
    @api_url,
    @method,
    @login,
    @verified_at)
RETURNING
    id,
    kind,
    host,
    api_url,
    method,
    login,
    verified_at,
    created_at;

-- name: GetForgeConnection :one
SELECT
    id,
    kind,
    host,
    api_url,
    method,
    login,
    verified_at,
    created_at
FROM
    forge_connections
WHERE
    id = @id;

-- name: GetForgeConnectionByHost :one
SELECT
    id,
    kind,
    host,
    api_url,
    method,
    login,
    verified_at,
    created_at
FROM
    forge_connections
WHERE
    host = @host;

-- name: ListForgeConnections :many
SELECT
    id,
    kind,
    host,
    api_url,
    method,
    login,
    verified_at,
    created_at
FROM
    forge_connections
ORDER BY
    host;

-- name: SetForgeConnectionVerifiedAt :execrows
UPDATE
    forge_connections
SET
    verified_at = sqlc.narg('verified_at')
WHERE
    id = @id;

-- name: DeleteForgeConnection :execrows
DELETE FROM forge_connections
WHERE id = @id;
