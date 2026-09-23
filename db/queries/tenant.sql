-- name: GetCurrentEngine :one
SELECT
    id,
    name
FROM
    tenants
WHERE
    id = current_tenant();

-- name: GetReaderIdentity :one
SELECT
    COALESCE(r.rolsuper, TRUE)::boolean AS superuser,
    COALESCE(r.rolbypassrls, TRUE)::boolean AS bypass_rls,
    current_login_is_reader()::boolean AS reader,
    COALESCE(current_tenant()::text, '')::text AS tenant_id
FROM
    pg_catalog.pg_roles r
WHERE
    r.rolname = SESSION_USER;
