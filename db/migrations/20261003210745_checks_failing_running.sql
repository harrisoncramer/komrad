-- Set comment to column: "latest_checks" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."latest_checks" IS 'Where the latest read said the checks on latest_head_sha stand, in komrad''s own words: pending, passing, failing or failing_running, null when it reported none.';
