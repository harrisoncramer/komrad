-- Create enum type "agent_role"
CREATE TYPE "agent_role" AS ENUM ('implementer', 'reviewer');
-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "role" "agent_role" NOT NULL DEFAULT 'implementer';
-- Set comment to column: "role" on table: "agents"
COMMENT ON COLUMN "agents"."role" IS 'What the agent was launched to do, which decides the half of the review briefing it is sent. A reviewer reads the work of the agent it is a child of and never writes any of its own.';
