DO $$
DECLARE
    stale text;
BEGIN
    FOR stale IN
    SELECT
        r.rolname
    FROM
        pg_roles r
    WHERE
        r.rolname LIKE 'weave\_engine\_t%'
        AND NOT EXISTS (
            SELECT
                1
            FROM
                pg_shdepend d
            WHERE
                d.refclassid = 'pg_authid'::regclass
                AND d.refobjid = r.oid)
        LOOP
            EXECUTE FORMAT('DROP ROLE IF EXISTS %I', stale);
        END LOOP;
END
$$;
