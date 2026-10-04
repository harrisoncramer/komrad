-- Modify "pull_request_checks" table
ALTER TABLE "pull_request_checks" ADD COLUMN "run_id" text NOT NULL DEFAULT '', ADD COLUMN "run_event" text NOT NULL DEFAULT '', ADD COLUMN "run_started_at" timestamptz NULL;
-- Set comment to column: "run_id" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."run_id" IS 'The forge''s own identifier for the run the check ran in, a check suite on GitHub, empty for a commit status.';
-- Set comment to column: "run_event" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."run_event" IS 'What started the run, such as pull_request or push, empty when no workflow ran the check.';
-- Set comment to column: "run_started_at" on table: "pull_request_checks"
COMMENT ON COLUMN "pull_request_checks"."run_started_at" IS 'When the run the check ran in was created, null for a commit status.';
