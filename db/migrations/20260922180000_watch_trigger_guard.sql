-- watch_trigger_is_current reports whether target already carries this trigger, running this
-- function with exactly these arguments.
CREATE OR REPLACE FUNCTION watch_trigger_is_current(target text, trigger_name text, function_name text, args text[])
    RETURNS boolean
    AS $$
DECLARE
    wanted bytea := ''::bytea;
    arg text;
BEGIN
    FOREACH arg IN ARRAY args LOOP
        wanted := wanted || convert_to(arg, 'UTF8') || '\x00'::bytea;
    END LOOP;
    RETURN EXISTS (
        SELECT
            1
        FROM
            pg_trigger
        WHERE
            tgrelid = TO_REGCLASS(target)
            AND tgname = trigger_name
            AND NOT tgisinternal
            AND tgfoid = function_name::regproc
            AND tgargs = wanted);
END;
$$
LANGUAGE plpgsql;

-- watch_project_lookup says the parents of a projected row moved, the parents being the rows of
-- the parent table whose column matches a column of this row. TG_ARGV: parent table, parent key
-- column, parent match column, this row's match column, a column of the parent that must be null.
CREATE OR REPLACE FUNCTION watch_project_lookup()
    RETURNS TRIGGER
    AS $$
DECLARE
    parent_key text;
    narrowing text := 'true';
BEGIN
    IF TG_ARGV[4] IS NOT NULL AND TG_ARGV[4] <> '' THEN
        narrowing := FORMAT('%I IS NULL', TG_ARGV[4]);
    END IF;
    FOR parent_key IN EXECUTE FORMAT('SELECT %I::text FROM %I WHERE %I::text = $1 AND (%s)', TG_ARGV[1], TG_ARGV[0], TG_ARGV[2], narrowing)
        USING (to_jsonb(COALESCE(NEW, OLD)) ->> TG_ARGV[3]) LOOP
        PERFORM pg_notify('weave_watch', TG_ARGV[0] || ' ' || parent_key);
    END LOOP;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

-- attach_watch_trigger puts watch_notify on a resource table, keyed by key_column, and does
-- nothing when the trigger is already what it would create.
CREATE OR REPLACE FUNCTION attach_watch_trigger(target text, key_column text)
    RETURNS void
    AS $$
BEGIN
    IF watch_trigger_is_current(target, 'watch_notify', 'watch_notify', ARRAY[key_column]) THEN
        RETURN;
    END IF;
    -- Creating a trigger locks the table.
    SET LOCAL lock_timeout = '5s';
    EXECUTE FORMAT('DROP TRIGGER IF EXISTS watch_notify ON %I', target);
    EXECUTE FORMAT('CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION watch_notify(%L)', target, key_column);
END;
$$
LANGUAGE plpgsql;

-- attach_projection_trigger puts watch_project on a table whose rows are rendered onto a parent
-- row, the parent's key held in parent_column, named after the parent so a table can feed more
-- than one.
CREATE OR REPLACE FUNCTION attach_projection_trigger(target text, parent text, parent_column text)
    RETURNS void
    AS $$
DECLARE
    trigger_name text := 'watch_project_' || parent;
BEGIN
    IF watch_trigger_is_current(target, trigger_name, 'watch_project', ARRAY[parent, parent_column]) THEN
        RETURN;
    END IF;
    SET LOCAL lock_timeout = '5s';
    EXECUTE FORMAT('DROP TRIGGER IF EXISTS %I ON %I', trigger_name, target);
    EXECUTE FORMAT('CREATE TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION watch_project(%L, %L)', trigger_name, target, parent, parent_column);
END;
$$
LANGUAGE plpgsql;

DROP FUNCTION IF EXISTS attach_projection_lookup_trigger(text, text, text, text, text, text);

-- attach_projection_lookup_trigger puts watch_project_lookup on a table whose parents are the
-- rows of parent whose parent_match column equals this table's match column, narrowed to the
-- parents whose parent_must_be_null is null when one is given.
CREATE OR REPLACE FUNCTION attach_projection_lookup_trigger(target text, parent text, parent_key text, parent_match text, match text, parent_must_be_null text)
    RETURNS void
    AS $$
DECLARE
    trigger_name text := 'watch_project_' || parent;
    narrowing text := COALESCE(parent_must_be_null, '');
BEGIN
    IF watch_trigger_is_current(target, trigger_name, 'watch_project_lookup', ARRAY[parent, parent_key, parent_match, match, narrowing]) THEN
        RETURN;
    END IF;
    SET LOCAL lock_timeout = '5s';
    EXECUTE FORMAT('DROP TRIGGER IF EXISTS %I ON %I', trigger_name, target);
    EXECUTE FORMAT('CREATE TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION watch_project_lookup(%L, %L, %L, %L, %L)', trigger_name, target, parent, parent_key, parent_match, match, narrowing);
END;
$$
LANGUAGE plpgsql;

-- Drops the lookup triggers attached in the old form.
DO $$
DECLARE
    stale record;
BEGIN
    FOR stale IN
    SELECT
        tgname,
        tgrelid::regclass AS target
    FROM
        pg_trigger
    WHERE
        NOT tgisinternal
        AND tgfoid = 'watch_project_lookup'::regproc LOOP
            EXECUTE FORMAT('DROP TRIGGER IF EXISTS %I ON %s', stale.tgname, stale.target);
        END LOOP;
END
$$;
