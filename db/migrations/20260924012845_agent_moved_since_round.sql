-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "moved_since_round" text NOT NULL DEFAULT '';
-- Set comment to column: "moved_since_round" on table: "agents"
COMMENT ON COLUMN "agents"."moved_since_round" IS 'The snapshot of the agent''s newest review round when its worktree no longer hashes to it, empty while it still does or when there is no round. The cwd syncer rewrites it every pass.';
