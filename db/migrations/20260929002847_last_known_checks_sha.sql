-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "last_known_checks_sha" text NULL;
-- Set comment to column: "last_known_checks_sha" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."last_known_checks_sha" IS 'The head commit last_known_checks was read for. The same checks on a new head are a fresh verdict, so an event compares this too.';
