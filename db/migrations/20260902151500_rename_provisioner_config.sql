-- Rename a table from "workflow_action_configs" to "workflow_provisioner_configs"
ALTER TABLE "workflow_action_configs" RENAME TO "workflow_provisioner_configs";
ALTER INDEX "workflow_action_configs_pkey" RENAME TO "workflow_provisioner_configs_pkey";
-- Modify "workflows" table
ALTER TABLE "workflows" RENAME COLUMN "action_config_id" TO "provisioner_config_id";
ALTER INDEX "workflows_action_config_id" RENAME TO "workflows_provisioner_config_id";
ALTER TABLE "workflows" RENAME CONSTRAINT "workflows_action_config_id_fkey" TO "workflows_provisioner_config_id_fkey";
-- Modify "workflow_hooks" table
ALTER TABLE "workflow_hooks" RENAME COLUMN "action" TO "script";
