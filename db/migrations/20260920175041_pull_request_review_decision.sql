-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "review_decision" text NULL, ADD COLUMN "decision_changed_at" timestamptz NULL;
-- Set comment to column: "review_decision" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."review_decision" IS 'Where the forge says the reviews stand, in weave''s own words: pending, approved or changes_requested, null while it has said nothing.';
-- Set comment to column: "decision_changed_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."decision_changed_at" IS 'The changed_at the review decision was read for, stale once changed_at has moved past it.';
