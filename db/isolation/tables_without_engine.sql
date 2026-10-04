SELECT
    c.relname::text
FROM
    pg_class c
WHERE
    c.relnamespace = 'public'::regnamespace
    AND c.relkind IN ('r', 'p')
    AND c.relname <> ALL ($1::text[])
    AND NOT EXISTS (
        SELECT
            1
        FROM
            pg_attribute a
        WHERE
            a.attrelid = c.oid
            AND a.attname = 'engine_id'
            AND a.attnotnull
            AND NOT a.attisdropped)
ORDER BY
    c.relname;
