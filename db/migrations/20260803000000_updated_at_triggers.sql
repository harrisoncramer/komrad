-- The trigger stamping updated_at, and the function that attaches it to every
-- table carrying the column. Neither is declared in db/schema, because this Atlas
-- edition does not manage functions or triggers, the same way it does not manage
-- extensions; the diff replays this migration into its dev database and leaves
-- both alone.
CREATE OR REPLACE FUNCTION set_updated_at ()
    RETURNS TRIGGER
    AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$
LANGUAGE plpgsql;

-- Attaching by looking for the column rather than by naming the tables, so the
-- set is read out of the schema at the moment it runs and cannot fall behind a
-- list somebody forgot to extend. A table added by a later migration is covered
-- by calling this again at the end of it, which the store test insists on.
CREATE OR REPLACE FUNCTION attach_updated_at_triggers ()
    RETURNS void
    AS $$
DECLARE
    target text;
BEGIN
    FOR target IN
    SELECT
        c.relname
    FROM
        pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_attribute a ON a.attrelid = c.oid
    WHERE
        n.nspname = 'public'
        AND c.relkind = 'r'
        AND a.attname = 'updated_at'
        AND NOT a.attisdropped LOOP
            EXECUTE format('DROP TRIGGER IF EXISTS set_updated_at ON %I', target);
            EXECUTE format('CREATE TRIGGER set_updated_at BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION set_updated_at()', target);
        END LOOP;
END;
$$
LANGUAGE plpgsql;

SELECT
    attach_updated_at_triggers ();
