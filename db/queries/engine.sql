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
