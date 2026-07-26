-- Create the pg_trgm extension, which the transcript search index depends on.
-- Written by hand rather than diffed: this Atlas edition does not manage
-- extensions, so the desired-state HCL cannot declare it.
CREATE EXTENSION IF NOT EXISTS "pg_trgm";
