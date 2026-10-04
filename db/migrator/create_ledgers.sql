CREATE TABLE IF NOT EXISTS schema_migrations(
    version text PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS schema_triggers(
    unit text PRIMARY KEY,
    version integer NOT NULL
);
