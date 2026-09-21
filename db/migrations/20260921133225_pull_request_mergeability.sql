-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "mergeability" text NULL;
-- Set comment to column: "checks_read_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."checks_read_at" IS 'When the checks and mergeability were last read, which is what paces the re-read of a pending or failing run.';
-- Set comment to column: "mergeability" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."mergeability" IS 'Where the forge says the pull request stands against its base and its branch rules, in weave''s own words: clean, unstable, blocked, behind or conflicting, null while the forge has not computed it. Read with the checks and kept fresh the same way.';
