-- Modify "workflow_configs" table
ALTER TABLE "workflow_configs" ADD COLUMN "reviewer_harness" text NOT NULL DEFAULT '', ADD COLUMN "reviewer_flags" text NOT NULL DEFAULT '';
-- Set comment to column: "reviewer_harness" on table: "workflow_configs"
COMMENT ON COLUMN "workflow_configs"."reviewer_harness" IS 'The harness the workflow''s reviewers run, blank meaning the one its other agents run.';
-- Set comment to column: "reviewer_flags" on table: "workflow_configs"
COMMENT ON COLUMN "workflow_configs"."reviewer_flags" IS 'The flags the workflow''s reviewers are launched with, blank meaning the other agents'' flags when the harness is the same.';
