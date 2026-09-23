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
