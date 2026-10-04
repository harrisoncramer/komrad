-- Set comment to column: "provider_id" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."provider_id" IS 'The forge''s own identifier for the check run, commit status or pipeline, which a rerun replaces.';
-- Set comment to column: "kind" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."kind" IS 'What the forge reported the check as: check_run for a check run, status for an old-style commit status, pipeline for a GitLab pipeline.';
