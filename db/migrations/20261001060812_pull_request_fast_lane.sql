-- Modify "pull_request_syncs" table
ALTER TABLE "pull_request_syncs" DROP COLUMN "updated_through", DROP COLUMN "listed_at", DROP COLUMN "sweep_cursor", DROP COLUMN "sweep_newest", DROP COLUMN "read_at", DROP COLUMN "calls";
-- Set comment to table: "pull_request_syncs"
COMMENT ON TABLE "pull_request_syncs" IS 'A repository the pull request sync has read at least once, which is what turns its pull request state from unread to none when it has no pull requests.';
-- Modify "pull_requests" table
ALTER TABLE "pull_requests" DROP COLUMN "decision_changed_at", DROP COLUMN "checks_read_at";
-- Set comment to column: "checks" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."checks" IS 'Where the forge says the checks on the head commit stand, in komrad''s own words: pending, passing or failing, null while it reports none.';
-- Set comment to column: "checks_sha" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."checks_sha" IS 'The head commit the checks were read for.';
-- Set comment to column: "mergeability" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."mergeability" IS 'Where the forge says the pull request stands against its base and its branch rules, in komrad''s own words: clean, unstable, blocked, behind or conflicting, null while the forge has not computed it. Read with the checks.';
-- Set comment to column: "auto_merge" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."auto_merge" IS 'Whether the forge says the pull request will merge on its own once its checks and reviews pass, null until it has been read. Read with the checks.';
-- Drop "branch_scans" table
DROP TABLE "branch_scans";
