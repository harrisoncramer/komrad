-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN "parent_agent_id";
-- Set comment to column: "role" on table: "agents"
COMMENT ON COLUMN "agents"."role" IS 'What the agent was launched to do. It decides which review briefing the agent is given. A reviewer reads the work of its task''s writer and never writes any code of its own.';
-- Rebuild enum type "agent_role" without "implementer"
ALTER TABLE "agents" DROP CONSTRAINT "agents_task_unless_orchestrator";
DROP INDEX "agents_workflow_orchestrator";
ALTER TABLE "agents" ALTER COLUMN "role" DROP DEFAULT, ALTER COLUMN "role" TYPE text;
DROP TYPE "agent_role";
CREATE TYPE "agent_role" AS ENUM ('writer', 'reviewer', 'orchestrator');
ALTER TABLE "agents" ALTER COLUMN "role" TYPE "agent_role" USING "role"::"agent_role", ALTER COLUMN "role" SET DEFAULT 'writer';
CREATE UNIQUE INDEX "agents_workflow_orchestrator" ON "agents" ("workflow_id") WHERE (role = 'orchestrator'::agent_role);
ALTER TABLE "agents" ADD CONSTRAINT "agents_task_unless_orchestrator" CHECK ((role = 'orchestrator'::agent_role) OR (task_id IS NOT NULL));
-- Rebuild enum type "schedule_kind" without "implementer"
ALTER TABLE "schedules" DROP CONSTRAINT "schedules_orchestrator_matches_kind";
ALTER TABLE "schedules" ALTER COLUMN "kind" DROP DEFAULT, ALTER COLUMN "kind" TYPE text;
DROP TYPE "schedule_kind";
CREATE TYPE "schedule_kind" AS ENUM ('task', 'orchestrator');
ALTER TABLE "schedules" ALTER COLUMN "kind" TYPE "schedule_kind" USING "kind"::"schedule_kind", ALTER COLUMN "kind" SET DEFAULT 'task';
ALTER TABLE "schedules" ADD CONSTRAINT "schedules_orchestrator_matches_kind" CHECK ((kind = 'orchestrator'::schedule_kind) = (orchestrator_agent_id IS NOT NULL));
