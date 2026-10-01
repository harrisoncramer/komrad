-- Modify "pull_requests" table
ALTER TABLE "pull_requests" DROP COLUMN "review_decision", DROP COLUMN "checks", DROP COLUMN "checks_sha", DROP COLUMN "mergeability", DROP COLUMN "last_known_checks", DROP COLUMN "last_known_mergeability", DROP COLUMN "last_known_decision", DROP COLUMN "last_known_checks_sha", DROP COLUMN "last_known_mergeability_sha", ADD COLUMN "read_decision" text NULL, ADD COLUMN "read_checks" text NULL, ADD COLUMN "read_head_sha" text NULL, ADD COLUMN "read_mergeability" text NULL, ADD COLUMN "known_checks" text NULL, ADD COLUMN "known_checks_sha" text NULL, ADD COLUMN "known_mergeability" text NULL, ADD COLUMN "known_mergeability_sha" text NULL, ADD COLUMN "known_decision" text NULL;
-- Set comment to column: "mergeability_blank_since" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."mergeability_blank_since" IS 'When the forge first answered a blank mergeability for read_head_sha, which bounds how long a blank is read again quickly.';
-- Set comment to column: "read_decision" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."read_decision" IS 'Where the latest read says the reviews stand, in komrad''s own words: pending, approved, changes_requested or rereview_requested, null when it said nothing.';
-- Set comment to column: "read_checks" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."read_checks" IS 'Where the latest read says the checks on read_head_sha stand, in komrad''s own words: pending, passing or failing, null when it reported none.';
-- Set comment to column: "read_head_sha" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."read_head_sha" IS 'The head commit the latest standing read was for, null before the first read.';
-- Set comment to column: "read_mergeability" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."read_mergeability" IS 'Where the latest read says the pull request stands against its base and its branch rules, in komrad''s own words: clean, unstable, blocked, behind or conflicting, null when the forge had not computed it.';
-- Set comment to column: "known_checks" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."known_checks" IS 'The last checks a read reported, which a blank read leaves alone.';
-- Set comment to column: "known_checks_sha" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."known_checks_sha" IS 'The head commit known_checks was read for.';
-- Set comment to column: "known_mergeability" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."known_mergeability" IS 'The last mergeability a read reported, which a blank read leaves alone.';
-- Set comment to column: "known_mergeability_sha" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."known_mergeability_sha" IS 'The head commit known_mergeability was read for.';
-- Set comment to column: "known_decision" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."known_decision" IS 'The last review decision a read reported, which a blank read leaves alone.';
