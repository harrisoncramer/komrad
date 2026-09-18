-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "repository_url" text NULL;
-- Set comment to column: "repository_url" on table: "agents"
COMMENT ON COLUMN "agents"."repository_url" IS 'The canonical web address of the repository the agent''s checkout pushes to, such as https://github.com/harrisoncramer/weave. Null when the checkout is gone or its origin is not a forge. The cwd syncer rewrites it on the same pass that rewrites branch.';
