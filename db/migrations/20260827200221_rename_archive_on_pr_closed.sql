-- Modify "workflow_action_configs" table
ALTER TABLE "workflow_action_configs" RENAME COLUMN "worktree_archive_on_pr_closed" TO "archive_on_pr_closed";
