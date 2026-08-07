-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "worktree_removed" boolean NOT NULL DEFAULT false;
