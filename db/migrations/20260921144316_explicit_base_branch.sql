-- Backfill "worktree_base_branch" with main for every workflow that named none.
UPDATE "workflow_provisioner_configs" SET "worktree_base_branch" = 'main' WHERE "worktree_base_branch" = '';
-- Modify "workflow_provisioner_configs" table
ALTER TABLE "workflow_provisioner_configs" ADD CONSTRAINT "worktree_base_branch_named" CHECK (worktree_base_branch <> ''::text), ALTER COLUMN "worktree_base_branch" DROP DEFAULT;
-- Set comment to column: "worktree_base_branch" on table: "workflow_provisioner_configs"
COMMENT ON COLUMN "workflow_provisioner_configs"."worktree_base_branch" IS 'The branch worktrees are cut from and every agent''s work is measured against, never blank.';
