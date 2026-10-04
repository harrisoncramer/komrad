-- Set comment to column: "state" on table: "pull_request_check_steps"
COMMENT ON COLUMN "pull_request_check_steps"."state" IS 'Where the step stands, in komrad''s own words: queued or running while in flight, waiting when it cannot start on its own, completed, or unknown when its commit is no longer the head and komrad stopped reading it while in flight.';
-- Set comment to column: "state" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."state" IS 'Where the check stands, in komrad''s own words: queued or running while in flight, waiting when it cannot start on its own, completed, or unknown when its commit is no longer the head and komrad stopped reading it while in flight.';
