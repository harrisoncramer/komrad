-- name: GetCurrentEngine :one
SELECT
    id,
    name
FROM
    engines
WHERE
    id = current_engine();

-- name: GetReaderIdentity :one
SELECT
    COALESCE(r.rolsuper, TRUE)::boolean AS superuser,
    COALESCE(r.rolbypassrls, TRUE)::boolean AS bypass_rls,
    current_login_is_reader()::boolean AS reader,
    COALESCE(current_engine()::text, '')::text AS engine_id
FROM
    pg_catalog.pg_roles r
WHERE
    r.rolname = SESSION_USER;

-- name: GetEngineIDByName :one
SELECT
    id
FROM
    engines
WHERE
    name = sqlc.arg(name);

-- name: GrantEngineAccess :exec
INSERT INTO engine_access(
    engine_id,
    email)
VALUES (
    sqlc.arg(engine_id),
    sqlc.arg(email))
ON CONFLICT (
    engine_id,
    email)
    DO NOTHING;

-- name: RevokeEngineAccess :execrows
DELETE FROM engine_access
WHERE engine_id = sqlc.arg(engine_id)
    AND email = sqlc.arg(email);
