-- name: ListIdentityTables :many
SELECT
    c.relname::text AS table_name
FROM
    pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE
    n.nspname = 'identity'
    AND c.relkind = 'r'
ORDER BY
    c.relname;

-- name: ListIdentityGrantsToEngines :many
WITH engine_roles AS (
    SELECT
        'public'::text AS role_name
    UNION
    SELECT
        g.rolname::text
    FROM
        pg_catalog.pg_roles g
    WHERE
        g.rolname IN ('komrad_engine', 'komrad_reader')
    UNION
    SELECT
        m.rolname::text
    FROM
        pg_catalog.pg_auth_members am
        JOIN pg_catalog.pg_roles g ON g.oid = am.roleid
        JOIN pg_catalog.pg_roles m ON m.oid = am.member
    WHERE
        g.rolname IN ('komrad_engine', 'komrad_reader')
    UNION
    SELECT
        r.rolname::text
    FROM
        pg_catalog.pg_roles r
    WHERE
        r.rolname LIKE 'komrad\_engine\_%'
),
targets AS (
    SELECT
        'schema identity'::text AS target,
        HAS_SCHEMA_PRIVILEGE(er.role_name, 'identity', 'USAGE, CREATE') AS granted,
        er.role_name
    FROM
        engine_roles er
    UNION ALL
    SELECT
        'table identity.' || c.relname,
        HAS_TABLE_PRIVILEGE(er.role_name, c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER')
        OR HAS_ANY_COLUMN_PRIVILEGE(er.role_name, c.oid, 'SELECT, INSERT, UPDATE, REFERENCES'),
        er.role_name
    FROM
        engine_roles er
        CROSS JOIN pg_catalog.pg_class c
        JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE
        n.nspname = 'identity'
)
SELECT
    (role_name || ' on ' || target)::text AS grant_name
FROM
    targets
WHERE
    granted
ORDER BY
    1;

-- name: ListSecurityDefinerFunctionsEnginesRun :many
WITH engine_roles AS (
    SELECT
        'public'::text AS role_name
    UNION
    SELECT
        g.rolname::text
    FROM
        pg_catalog.pg_roles g
    WHERE
        g.rolname IN ('komrad_engine', 'komrad_reader')
    UNION
    SELECT
        m.rolname::text
    FROM
        pg_catalog.pg_auth_members am
        JOIN pg_catalog.pg_roles g ON g.oid = am.roleid
        JOIN pg_catalog.pg_roles m ON m.oid = am.member
    WHERE
        g.rolname IN ('komrad_engine', 'komrad_reader')
    UNION
    SELECT
        r.rolname::text
    FROM
        pg_catalog.pg_roles r
    WHERE
        r.rolname LIKE 'komrad\_engine\_%'
)
SELECT DISTINCT
    (n.nspname || '.' || p.proname)::text AS function_name
FROM
    pg_catalog.pg_proc p
    JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
    CROSS JOIN engine_roles er
WHERE
    p.prosecdef
    AND HAS_FUNCTION_PRIVILEGE(er.role_name, p.oid, 'EXECUTE')
ORDER BY
    1;

-- name: ListPrincipalRoleValues :many
SELECT
    e.enumlabel::text AS role
FROM
    pg_catalog.pg_enum e
WHERE
    e.enumtypid = 'identity.principal_role'::regtype
ORDER BY
    e.enumsortorder;

-- name: DeleteEngineDocumentTypes :exec
DELETE FROM document_types
WHERE engine_id::text = @id::text;

-- name: DeleteEngine :exec
DELETE FROM engines
WHERE id::text = @id::text;

-- name: CountPrincipals :one
SELECT
    COUNT(*)
FROM
    identity.principals;
