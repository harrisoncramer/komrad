-- watch_notify says a resource row moved: the table and the row's id, delivered on commit.
CREATE OR REPLACE FUNCTION watch_notify()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM pg_notify('weave_watch', TG_TABLE_NAME || ' ' || OLD.id::text);
    ELSE
        PERFORM pg_notify('weave_watch', TG_TABLE_NAME || ' ' || NEW.id::text);
    END IF;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

-- watch_project_lookup says the parents of a projected row moved, the parents being the rows of
-- the parent table whose column matches a column of this row. TG_ARGV: parent table, parent match
-- column, this row's match column, a column of the parent that must be null.
CREATE OR REPLACE FUNCTION watch_project_lookup()
    RETURNS TRIGGER
    AS $$
DECLARE
    parent_id text;
    narrowing text := 'true';
BEGIN
    IF TG_ARGV[3] IS NOT NULL AND TG_ARGV[3] <> '' THEN
        narrowing := FORMAT('%I IS NULL', TG_ARGV[3]);
    END IF;
    FOR parent_id IN EXECUTE FORMAT('SELECT id::text FROM %I WHERE %I::text = $1 AND (%s)', TG_ARGV[0], TG_ARGV[1], narrowing)
        USING (to_jsonb(COALESCE(NEW, OLD)) ->> TG_ARGV[2]) LOOP
        PERFORM pg_notify('weave_watch', TG_ARGV[0] || ' ' || parent_id);
    END LOOP;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;
