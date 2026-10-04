INSERT INTO schema_triggers(
    unit,
    version)
VALUES (
    $1,
    $2)
ON CONFLICT (
    unit)
    DO UPDATE SET
        version = EXCLUDED.version;
