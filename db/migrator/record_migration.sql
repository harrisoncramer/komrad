WITH recorded AS (
    INSERT INTO schema_migrations(
        version)
    VALUES (
        $1))
DELETE FROM schema_triggers
WHERE unit = ANY ($2);
