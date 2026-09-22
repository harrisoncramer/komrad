-- watch_notify says a resource row moved: the table and the key, delivered on commit.
-- TG_ARGV[0] is the key column.
CREATE OR REPLACE FUNCTION watch_notify()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF TG_OP <> 'INSERT' AND (to_jsonb(OLD) ->> TG_ARGV[0]) IS NOT NULL THEN
        PERFORM pg_notify('weave_watch', TG_TABLE_NAME || ' ' || (to_jsonb(OLD) ->> TG_ARGV[0]));
    END IF;
    IF TG_OP <> 'DELETE' AND (to_jsonb(NEW) ->> TG_ARGV[0]) IS NOT NULL THEN
        PERFORM pg_notify('weave_watch', TG_TABLE_NAME || ' ' || (to_jsonb(NEW) ->> TG_ARGV[0]));
    END IF;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

-- watch_project says the parent of a projected row moved, the parent's key being a column of
-- the row. TG_ARGV: parent table, the column on this row.
CREATE OR REPLACE FUNCTION watch_project()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF TG_OP <> 'INSERT' AND (to_jsonb(OLD) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify('weave_watch', TG_ARGV[0] || ' ' || (to_jsonb(OLD) ->> TG_ARGV[1]));
    END IF;
    IF TG_OP <> 'DELETE' AND (to_jsonb(NEW) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify('weave_watch', TG_ARGV[0] || ' ' || (to_jsonb(NEW) ->> TG_ARGV[1]));
    END IF;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

-- watch_project_lookup says the parents of a projected row moved, the parents being the rows of
-- the parent table whose column matches a column of this row. TG_ARGV: parent table, parent key
-- column, parent match column, this row's match column, an extra WHERE.
CREATE OR REPLACE FUNCTION watch_project_lookup()
    RETURNS TRIGGER
    AS $$
DECLARE
    parent_key text;
BEGIN
    FOR parent_key IN EXECUTE FORMAT('SELECT %I::text FROM %I WHERE %I::text = $1 AND (%s)', TG_ARGV[1], TG_ARGV[0], TG_ARGV[2], TG_ARGV[4])
        USING (to_jsonb(COALESCE(NEW, OLD)) ->> TG_ARGV[3]) LOOP
        PERFORM pg_notify('weave_watch', TG_ARGV[0] || ' ' || parent_key);
    END LOOP;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

-- attach_watch_trigger puts watch_notify on a resource table, keyed by key_column.
CREATE OR REPLACE FUNCTION attach_watch_trigger(target text, key_column text)
    RETURNS void
    AS $$
BEGIN
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
BEGIN
    EXECUTE FORMAT('DROP TRIGGER IF EXISTS watch_project_%I ON %I', parent, target);
    EXECUTE FORMAT('CREATE TRIGGER watch_project_%I AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION watch_project(%L, %L)', parent, target, parent, parent_column);
END;
$$
LANGUAGE plpgsql;

-- attach_projection_lookup_trigger puts watch_project_lookup on a table whose parents are the
-- rows of parent whose parent_match column equals this table's match column, narrowed by an
-- extra WHERE when one is given, named after the parent the same way.
CREATE OR REPLACE FUNCTION attach_projection_lookup_trigger(target text, parent text, parent_key text, parent_match text, match text, extra_where text)
    RETURNS void
    AS $$
BEGIN
    EXECUTE FORMAT('DROP TRIGGER IF EXISTS watch_project_%I ON %I', parent, target);
    EXECUTE FORMAT('CREATE TRIGGER watch_project_%I AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION watch_project_lookup(%L, %L, %L, %L, %L)', parent, target, parent, parent_key, parent_match, match, COALESCE(extra_where, 'true'));
END;
$$
LANGUAGE plpgsql;
