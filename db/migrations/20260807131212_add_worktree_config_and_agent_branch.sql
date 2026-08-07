-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "branch" text NULL;
-- Modify "workflows" table
ALTER TABLE "workflows" ADD COLUMN "worktree_enabled" boolean NOT NULL DEFAULT false, ADD COLUMN "worktree_base_branch" text NOT NULL DEFAULT '', ADD COLUMN "worktree_copy_paths" text[] NOT NULL DEFAULT '{}', ADD COLUMN "worktree_symlink_paths" text[] NOT NULL DEFAULT '{}';
