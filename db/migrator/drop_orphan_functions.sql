DO $$
DECLARE
    orphan regprocedure;
BEGIN
    FOR orphan IN
    SELECT
        p.oid::regprocedure
    FROM
        pg_proc p
    WHERE
        p.pronamespace = 'public'::regnamespace
        AND p.prorettype = 'trigger'::regtype
        AND NOT EXISTS (
            SELECT
                1
            FROM
                pg_trigger t
            WHERE
                t.tgfoid = p.oid)
            LOOP
                EXECUTE FORMAT('DROP FUNCTION %s', orphan);
            END LOOP;
END;
$$;
