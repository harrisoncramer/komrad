-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "pushed_sha" text NULL, ADD COLUMN "pushed_at" timestamptz NULL;
-- Set comment to column: "pushed_sha" on table: "agents"
COMMENT ON COLUMN "agents"."pushed_sha" IS 'The commit the checkout last saw origin''s copy of its branch at, which moves when the branch is pushed or fetched. Null when origin has no such branch or the checkout is gone.';
-- Set comment to column: "pushed_at" on table: "agents"
COMMENT ON COLUMN "agents"."pushed_at" IS 'When the cwd syncer saw pushed_sha move. The pull request sync reads the standing of the branch''s pull request directly while the forge has not yet answered for that commit, rather than waiting for search to notice the push.';
