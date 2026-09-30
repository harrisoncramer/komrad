-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "last_known_mergeability_sha" text NULL;
-- Set comment to column: "last_known_mergeability_sha" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."last_known_mergeability_sha" IS 'The head commit last_known_mergeability was read for. The panel draws the last known mergeability through a blank reading only while this is still the head.';
