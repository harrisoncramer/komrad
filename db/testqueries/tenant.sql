-- name: ListTablesWithoutTenant :many
SELECT
    c.relname::text AS table_name
FROM
    pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE
    n.nspname = 'public'
    AND c.relkind = 'r'
    AND c.relname NOT IN ('tenants', 'schema_migrations')
    AND NOT EXISTS (
        SELECT
            1
        FROM
            pg_catalog.pg_attribute a
        WHERE
            a.attrelid = c.oid
            AND a.attname = 'tenant_id'
            AND a.attnotnull
            AND NOT a.attisdropped)
ORDER BY
    c.relname;

-- name: ListTablesWithoutEngineIsolation :many
SELECT
    c.relname::text AS table_name
FROM
    pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
WHERE
    n.nspname = 'public'
    AND c.relkind = 'r'
    AND a.attname = 'tenant_id'
    AND NOT a.attisdropped
    AND c.relname <> 'tenant_logins'
    AND (NOT c.relrowsecurity
        OR NOT EXISTS (
            SELECT
                1
            FROM
                pg_catalog.pg_policy p
            WHERE
                p.polrelid = c.oid
                AND p.polname = 'engine_isolation')
            OR NOT EXISTS (
                SELECT
                    1
                FROM
                    pg_catalog.pg_trigger t
                WHERE
                    t.tgrelid = c.oid
                    AND t.tgname = 'stamp_tenant'))
    ORDER BY
        c.relname;

-- name: ListForeignKeysWithoutTenant :many
SELECT
    con.conname::text AS constraint_name
FROM
    pg_catalog.pg_constraint con
    JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_catalog.pg_class ref ON ref.oid = con.confrelid
    JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
        AND a.attname = 'tenant_id'
WHERE
    n.nspname = 'public'
    AND con.contype = 'f'
    AND ref.relname <> 'tenants'
    AND NOT (a.attnum = ANY (con.conkey))
ORDER BY
    con.conname;

-- name: ListSetNullKeysClearingTenant :many
SELECT
    con.conname::text AS constraint_name
FROM
    pg_catalog.pg_constraint con
    JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
        AND a.attname = 'tenant_id'
WHERE
    n.nspname = 'public'
    AND con.contype = 'f'
    AND con.confdeltype = 'n'
    AND (con.confdelsetcols IS NULL
        OR a.attnum = ANY (con.confdelsetcols))
ORDER BY
    con.conname;

-- name: ListUniqueKeysAcrossEngines :many
SELECT
    i.relname::text AS index_name
FROM
    pg_catalog.pg_index x
    JOIN pg_catalog.pg_class i ON i.oid = x.indexrelid
    JOIN pg_catalog.pg_class c ON c.oid = x.indrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_catalog.pg_attribute lead ON lead.attrelid = c.oid
        AND lead.attnum = x.indkey[0]
WHERE
    n.nspname = 'public'
    AND x.indisunique
    AND c.relname NOT IN ('tenants', 'tenant_logins', 'schema_migrations')
    AND lead.attname <> 'tenant_id'
    AND lead.atttypid <> 'uuid'::regtype
ORDER BY
    i.relname;

-- name: ListTablesEnginesCannotReach :many
SELECT
    c.relname::text AS table_name
FROM
    pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
WHERE
    n.nspname = 'public'
    AND c.relkind = 'r'
    AND a.attname = 'tenant_id'
    AND NOT a.attisdropped
    AND c.relname <> 'tenant_logins'
    AND NOT (HAS_TABLE_PRIVILEGE('komrade_engine', c.oid, 'SELECT')
        AND HAS_TABLE_PRIVILEGE('komrade_engine', c.oid, 'INSERT')
        AND HAS_TABLE_PRIVILEGE('komrade_engine', c.oid, 'UPDATE')
        AND HAS_TABLE_PRIVILEGE('komrade_engine', c.oid, 'DELETE')
        AND HAS_TABLE_PRIVILEGE('komrade_reader', c.oid, 'SELECT'))
ORDER BY
    c.relname;
