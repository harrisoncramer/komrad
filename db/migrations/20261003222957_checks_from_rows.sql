-- Modify "pull_requests" table
ALTER TABLE "pull_requests" DROP COLUMN "latest_checks", DROP COLUMN "known_checks", DROP COLUMN "known_checks_sha";
-- Modify "pull_request_checks" table
ALTER TABLE "pull_request_checks" ADD COLUMN "pull_request_id" uuid NOT NULL, ADD CONSTRAINT "pull_request_checks_pull_request_id_fkey" FOREIGN KEY ("engine_id", "pull_request_id") REFERENCES "pull_requests" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "pull_request_checks_pull_request_id" to table: "pull_request_checks"
CREATE INDEX "pull_request_checks_pull_request_id" ON "pull_request_checks" ("pull_request_id");
-- Set comment to column: "provider_id" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."provider_id" IS 'The forge''s own identifier for the check run, commit status or pipeline, which a rerun replaces.';
-- Set comment to column: "kind" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."kind" IS 'What the forge reported the check as: check_run for a check run, status for an old-style commit status, pipeline for a GitLab pipeline.';
-- Set comment to column: "pull_request_id" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."pull_request_id" IS 'The pull request the check was listed on, which its reading also names.';
