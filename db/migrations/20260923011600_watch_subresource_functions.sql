-- watch_subresource says the resource a subresource row belongs to moved, the resource's key
-- being a column of the row. TG_ARGV: resource table, the column on this row.
CREATE OR REPLACE FUNCTION watch_subresource()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF TG_OP <> 'INSERT' AND (to_jsonb(OLD) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify('komrade_watch', TG_ARGV[0] || ' ' || (to_jsonb(OLD) ->> TG_ARGV[1]));
    END IF;
    IF TG_OP <> 'DELETE' AND (to_jsonb(NEW) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify('komrade_watch', TG_ARGV[0] || ' ' || (to_jsonb(NEW) ->> TG_ARGV[1]));
    END IF;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

-- watch_subresource_lookup says the resources a subresource row belongs to moved, those being the
-- rows of the resource table whose column matches a column of this row. TG_ARGV: resource table,
-- resource match column, this row's match column, a column of the resource that must be null.
CREATE OR REPLACE FUNCTION watch_subresource_lookup()
    RETURNS TRIGGER
    AS $$
DECLARE
    resource_id text;
    narrowing text := 'true';
BEGIN
    IF TG_ARGV[3] IS NOT NULL AND TG_ARGV[3] <> '' THEN
        narrowing := FORMAT('%I IS NULL', TG_ARGV[3]);
    END IF;
    FOR resource_id IN EXECUTE FORMAT('SELECT id::text FROM %I WHERE %I::text = $1 AND (%s)', TG_ARGV[0], TG_ARGV[1], narrowing)
        USING (to_jsonb(COALESCE(NEW, OLD)) ->> TG_ARGV[2]) LOOP
        PERFORM pg_notify('komrade_watch', TG_ARGV[0] || ' ' || resource_id);
    END LOOP;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

DROP FUNCTION watch_project() CASCADE;

DROP FUNCTION watch_project_lookup() CASCADE;
