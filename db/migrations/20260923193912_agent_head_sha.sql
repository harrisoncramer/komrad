-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "head_sha" text NULL;
-- Set comment to column: "head_sha" on table: "agents"
COMMENT ON COLUMN "agents"."head_sha" IS 'The commit the agent''s checkout has checked out, null when the checkout is gone or git cannot read it. The cwd syncer rewrites it on the same pass that rewrites branch.';
