DROP FUNCTION IF EXISTS attach_updated_at_triggers();

DO $$
BEGIN
    IF to_regclass('public.schema_triggers') IS NOT NULL THEN
        REVOKE INSERT, UPDATE, DELETE ON schema_triggers FROM komrad_engine;
    END IF;
END;
$$;
