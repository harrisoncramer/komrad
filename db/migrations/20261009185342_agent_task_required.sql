-- Modify "agents" table
ALTER TABLE "agents" ADD CONSTRAINT "agents_task_unless_orchestrator" CHECK ((role = 'orchestrator'::agent_role) OR (task_id IS NOT NULL)) NOT VALID;
-- Set comment to column: "task_id" on table: "agents"
COMMENT ON COLUMN "agents"."task_id" IS 'The task the agent works on, null only for an orchestrator.';
