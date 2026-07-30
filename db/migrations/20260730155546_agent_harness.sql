-- Modify "agents" table
ALTER TABLE "agents" RENAME COLUMN "claude_session_id" TO "session_id";
ALTER TABLE "agents" ADD COLUMN "harness" text NOT NULL DEFAULT 'claude';
-- Modify "workflow_configs" table
ALTER TABLE "workflow_configs" ADD COLUMN "harness" text NOT NULL DEFAULT '';
-- Set comment to table: "workflow_configs"
COMMENT ON TABLE "workflow_configs" IS 'Default config (harness, cwd, flags) seeded into new agents of a workflow.';
