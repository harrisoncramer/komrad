-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "failure_reason" text NOT NULL DEFAULT '';
-- Set comment to column: "failure_reason" on table: "agents"
COMMENT ON COLUMN "agents"."failure_reason" IS 'Why the agent''s create or cleanup failed, empty unless it is in one of the failed states.';
-- Drop "agent_metadata" table
DROP TABLE "agent_metadata";
