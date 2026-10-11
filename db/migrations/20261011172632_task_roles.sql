-- Add value to enum type: "agent_role"
ALTER TYPE "agent_role" ADD VALUE IF NOT EXISTS 'writer' AFTER 'implementer';
-- Add value to enum type: "schedule_kind"
ALTER TYPE "schedule_kind" ADD VALUE IF NOT EXISTS 'task' AFTER 'implementer';
-- Create index "agents_requested_idempotency_key" to table: "agents"
CREATE UNIQUE INDEX "agents_requested_idempotency_key" ON "agents" ("engine_id", "requested_by_agent_id", "idempotency_key") NULLS NOT DISTINCT WHERE (idempotency_key IS NOT NULL);
-- Set comment to table: "schedules"
COMMENT ON TABLE "schedules" IS 'A recurring firing, holding when to fire and the prompt it starts a task with or mails an orchestrator.';
-- Set comment to column: "kind" on table: "schedules"
COMMENT ON COLUMN "schedules"."kind" IS 'Whether a firing starts a task in the workflow or mails the prompt to an orchestrator.';
