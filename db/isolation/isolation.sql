SET LOCAL lock_timeout = '5s';

CREATE OR REPLACE FUNCTION stamp_engine()
    RETURNS TRIGGER
    AS $$
BEGIN
    NEW.engine_id := COALESCE(current_engine(), NEW.engine_id);
    RETURN NEW;
END;
$$
LANGUAGE plpgsql;

DO $$
DECLARE
    unisolated_tables text[] := STRING_TO_ARRAY(CURRENT_SETTING('komrad.unisolated_tables'), ',');
    readerless_tables text[] := STRING_TO_ARRAY(CURRENT_SETTING('komrad.readerless_tables'), ',');
    engine_table record;
BEGIN
    FOR engine_table IN
    SELECT
        *
    FROM (
        SELECT
            c.relname::text AS name,
            c.relname <> ALL (unisolated_tables) AS should_isolate,
            c.relname <> ALL (readerless_tables) AS should_reader_read,
            c.relrowsecurity AS has_row_security,
            EXISTS (
                SELECT
                    1
                FROM
                    pg_policy p
                WHERE
                    p.polrelid = c.oid
                    AND p.polname = 'engine_isolation') AS has_isolation_policy,
                EXISTS (
                    SELECT
                        1
                    FROM
                        pg_trigger t
                    WHERE
                        t.tgrelid = c.oid
                        AND t.tgname = 'stamp_engine'
                        AND NOT t.tgisinternal) AS has_stamp_trigger,
                    HAS_TABLE_PRIVILEGE('komrad_engine', c.oid, 'SELECT')
                    AND HAS_TABLE_PRIVILEGE('komrad_engine', c.oid, 'INSERT')
                    AND HAS_TABLE_PRIVILEGE('komrad_engine', c.oid, 'UPDATE')
                    AND HAS_TABLE_PRIVILEGE('komrad_engine', c.oid, 'DELETE') AS engine_can_write,
                    HAS_TABLE_PRIVILEGE('komrad_engine', c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER') AS engine_has_any_grant,
                    HAS_TABLE_PRIVILEGE('komrad_reader', c.oid, 'SELECT') AS reader_can_read,
                    HAS_TABLE_PRIVILEGE('komrad_reader', c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER') AS reader_has_any_grant
                FROM
                    pg_class c
                    JOIN pg_attribute a ON a.attrelid = c.oid
                WHERE
                    c.relnamespace = 'public'::regnamespace
                    AND c.relkind IN ('r', 'p')
                    AND a.attname = 'engine_id'
                    AND NOT a.attisdropped) engine_tables
WHERE
    NOT CASE WHEN should_isolate THEN
        has_row_security
        AND has_isolation_policy
        AND has_stamp_trigger
        AND engine_can_write
        AND reader_can_read = should_reader_read
    ELSE
        NOT has_row_security
        AND NOT has_isolation_policy
        AND NOT has_stamp_trigger
        AND NOT engine_has_any_grant
        AND NOT reader_has_any_grant
    END
ORDER BY
    name LOOP
        IF NOT engine_table.should_isolate THEN
            EXECUTE FORMAT('DROP POLICY IF EXISTS engine_isolation ON %I', engine_table.name);
            EXECUTE FORMAT('DROP TRIGGER IF EXISTS stamp_engine ON %I', engine_table.name);
            EXECUTE FORMAT('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', engine_table.name);
            EXECUTE FORMAT('REVOKE ALL ON %I FROM komrad_engine, komrad_reader', engine_table.name);
            CONTINUE;
        END IF;
        EXECUTE FORMAT('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', engine_table.name);
        EXECUTE FORMAT('DROP POLICY IF EXISTS engine_isolation ON %I', engine_table.name);
        EXECUTE FORMAT('CREATE POLICY engine_isolation ON %I USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()))', engine_table.name);
        EXECUTE FORMAT('CREATE OR REPLACE TRIGGER stamp_engine BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION stamp_engine()', engine_table.name);
        EXECUTE FORMAT('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO komrad_engine', engine_table.name);
        IF engine_table.should_reader_read THEN
            EXECUTE FORMAT('GRANT SELECT ON %I TO komrad_reader', engine_table.name);
        ELSE
            EXECUTE FORMAT('REVOKE ALL ON %I FROM komrad_reader', engine_table.name);
        END IF;
    END LOOP;
END
$$;
