CREATE EXTENSION IF NOT EXISTS "pg_trgm";

CREATE TEXT SEARCH DICTIONARY "english_nostop" (TEMPLATE = snowball, Language = english);
CREATE TEXT SEARCH CONFIGURATION "english_nostop" (COPY = english);
ALTER TEXT SEARCH CONFIGURATION "english_nostop" ALTER MAPPING FOR asciiword, asciihword, hword_asciipart, word, hword, hword_part WITH "english_nostop";

CREATE OR REPLACE FUNCTION set_updated_at()
    RETURNS TRIGGER
    AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$
LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION attach_updated_at_triggers()
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
            EXECUTE FORMAT('DROP TRIGGER IF EXISTS set_updated_at ON %I', target);
            EXECUTE FORMAT('CREATE TRIGGER set_updated_at BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION set_updated_at()', target);
        END LOOP;
END;
$$
LANGUAGE plpgsql;

-- watch_notify says a resource row moved: the table and the row's id, delivered on commit.
CREATE OR REPLACE FUNCTION watch_notify()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM pg_notify('komrad_watch', TG_TABLE_NAME || ' ' || OLD.id::text);
    ELSE
        PERFORM pg_notify('komrad_watch', TG_TABLE_NAME || ' ' || NEW.id::text);
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
        PERFORM pg_notify('komrad_watch', TG_ARGV[0] || ' ' || (to_jsonb(OLD) ->> TG_ARGV[1]));
    END IF;
    IF TG_OP <> 'DELETE' AND (to_jsonb(NEW) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify('komrad_watch', TG_ARGV[0] || ' ' || (to_jsonb(NEW) ->> TG_ARGV[1]));
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
        PERFORM pg_notify('komrad_watch', TG_ARGV[0] || ' ' || parent_id);
    END LOOP;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;
