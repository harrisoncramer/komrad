-- Create enum type "schedule_kind"
CREATE TYPE "schedule_kind" AS ENUM ('implementer', 'orchestrator');
-- Modify "schedules" table
ALTER TABLE "schedules" ADD CONSTRAINT "schedules_orchestrator_matches_kind" CHECK ((kind = 'orchestrator'::schedule_kind) = (orchestrator_agent_id IS NOT NULL)), ADD COLUMN "kind" "schedule_kind" NOT NULL DEFAULT 'implementer', ADD COLUMN "orchestrator_agent_id" uuid NULL;
-- Set comment to table: "schedules"
COMMENT ON TABLE "schedules" IS 'A recurring firing, holding when to fire and the prompt it launches an implementer with or mails an orchestrator.';
-- Set comment to column: "kind" on table: "schedules"
COMMENT ON COLUMN "schedules"."kind" IS 'Whether a firing launches an implementer into the workflow or mails the prompt to an orchestrator.';
-- Set comment to column: "orchestrator_agent_id" on table: "schedules"
COMMENT ON COLUMN "schedules"."orchestrator_agent_id" IS 'The orchestrator an orchestrator schedule mails, left unconstrained so the agent can be deleted while the schedule names it.';
