DO $$
DECLARE
    login_name text := CURRENT_SETTING('weave.engine_login');
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[login_name, login_name || '_reader']
    LOOP
        IF EXISTS (
            SELECT
                1
            FROM
                pg_roles
            WHERE
                rolname = target) THEN
        EXECUTE FORMAT('DROP OWNED BY %I', target);
        EXECUTE FORMAT('DROP ROLE %I', target);
    END IF;
END LOOP;
END
$$;
