-- Modify "agents" table
ALTER TABLE "agents" RENAME COLUMN "setup_action" TO "provision_action";
ALTER TABLE "agents" RENAME COLUMN "cleanup_action" TO "deprovision_action";
-- Modify "workflow_action_configs" table
ALTER TABLE "workflow_action_configs" RENAME COLUMN "setup_action" TO "provision_action";
ALTER TABLE "workflow_action_configs" RENAME COLUMN "cleanup_action" TO "deprovision_action";
-- Set comment to table: "workflow_action_configs"
COMMENT ON TABLE "workflow_action_configs" IS 'The provisioner that owns the directory of a workflow''s agents, and the worktree config it can be turned on for instead.';
