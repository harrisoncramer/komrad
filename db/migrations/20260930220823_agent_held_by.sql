-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "held_by_agent_id" uuid NULL;
-- Set comment to column: "held_by_agent_id" on table: "agents"
COMMENT ON COLUMN "agents"."held_by_agent_id" IS 'The agent that took the hold, null when the panel took it.';
