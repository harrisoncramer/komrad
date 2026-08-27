-- Modify "workflow_action_configs" table
ALTER TABLE "workflow_action_configs" ADD COLUMN "worktree_diff_exclude" text[] NOT NULL DEFAULT '{}';
