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
    LOWER(
        sqlc.arg(email)))
ON CONFLICT (
    engine_id,
    email)
    DO NOTHING;

-- name: RevokeEngineAccess :execrows
DELETE FROM engine_access
WHERE engine_id = sqlc.arg(engine_id)
    AND email = LOWER(sqlc.arg(email));

-- name: ClaimEngineAccess :one
SELECT
    claim_engine_access(sqlc.arg(email), sqlc.arg(issuer), sqlc.arg(subject))::boolean AS claimed;

-- name: IsEngineAccessGranted :one
SELECT
    engine_access_granted(sqlc.arg(issuer), sqlc.arg(subject))::boolean AS granted;

-- name: IsEngineGranted :one
SELECT
    EXISTS (
        SELECT
        FROM
            engine_access a
            JOIN engines e ON e.id = a.engine_id
        WHERE
            e.name = sqlc.arg(engine)
            AND ((a.issuer = sqlc.arg(issuer)::text
                    AND a.subject = sqlc.arg(subject)::text)
                OR (sqlc.arg(email_verified)::boolean
                    AND a.subject IS NULL
                    AND a.email = LOWER(sqlc.arg(email)))))::boolean AS granted;

-- name: RevokeOtherEngineAccess :exec
DELETE FROM engine_access
WHERE engine_id = sqlc.arg(engine_id)
    AND email <> LOWER(sqlc.arg(email));

-- name: RevokeEngineAccessOutside :exec
DELETE FROM engine_access a USING engines e
WHERE e.id = a.engine_id
    AND NOT (e.name = ANY (sqlc.arg(names)::text[]));
