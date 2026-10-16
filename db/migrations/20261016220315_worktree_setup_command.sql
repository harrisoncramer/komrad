-- Modify "workflow_provisioner_configs" table
ALTER TABLE "workflow_provisioner_configs" ADD COLUMN "worktree_setup_command" text NOT NULL DEFAULT '';
-- Set comment to column: "worktree_setup_command" on table: "workflow_provisioner_configs"
COMMENT ON COLUMN "workflow_provisioner_configs"."worktree_setup_command" IS 'The shell command run in each worktree before an agent starts or resumes there, blank for none.';
