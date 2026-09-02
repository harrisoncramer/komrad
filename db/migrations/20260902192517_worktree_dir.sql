-- Modify "workflow_provisioner_configs" table
ALTER TABLE "workflow_provisioner_configs" ADD COLUMN "worktree_dir" text NOT NULL DEFAULT '';
