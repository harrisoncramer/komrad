-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "author" text NOT NULL DEFAULT '', ADD COLUMN "body" text NOT NULL DEFAULT '';
-- Set comment to column: "author" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."author" IS 'The login of whoever opened it, empty when the forge no longer has them.';
-- Set comment to column: "body" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."body" IS 'The description as the forge has it, in markdown.';
