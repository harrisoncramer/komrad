-- Modify "workflow_action_configs" table
ALTER TABLE "workflow_action_configs" ADD COLUMN "worktree_archive_on_pr_closed" boolean NOT NULL DEFAULT false;
