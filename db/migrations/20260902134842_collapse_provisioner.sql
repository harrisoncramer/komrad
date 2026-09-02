-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "provision_enabled" boolean NULL;
UPDATE "agents" SET "provision_enabled" = FALSE WHERE "provision_action" = '';
ALTER TABLE "agents" DROP COLUMN "provision_action", DROP COLUMN "deprovision_action";
-- Modify "workflow_action_configs" table
ALTER TABLE "workflow_action_configs" DROP COLUMN "provision_action", DROP COLUMN "deprovision_action";
-- Set comment to table: "workflow_action_configs"
COMMENT ON TABLE "workflow_action_configs" IS 'The worktree config a workflow provisions its agents with, and whether it provisions them at all.';
