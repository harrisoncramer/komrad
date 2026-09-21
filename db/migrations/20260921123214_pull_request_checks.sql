-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "checks" text NULL, ADD COLUMN "checks_sha" text NULL, ADD COLUMN "checks_read_at" timestamptz NULL;
-- Set comment to column: "checks" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."checks" IS 'Where the forge says the checks on the head commit stand, in weave''s own words: pending, passing or failing, null while it reports none. A check finishing does not move changed_at, so the checks are kept fresh by checks_sha and checks_read_at rather than by the change stamp.';
-- Set comment to column: "checks_sha" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."checks_sha" IS 'The head commit the checks were read for, stale once head_sha has moved past it.';
-- Set comment to column: "checks_read_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."checks_read_at" IS 'When the checks were last read, which is what paces the re-read of a pending or failing run.';
