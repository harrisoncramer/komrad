-- Create enum type "agent_state"
CREATE TYPE "agent_state" AS ENUM ('idle', 'thinking', 'waiting', 'setting_up', 'failed_to_create', 'failed_to_cleanup');
-- Modify "agents" table
-- The default is dropped first because Postgres will not cast an existing text default to the
-- new type on its own, and put back afterwards as a value of it.
ALTER TABLE "agents" ALTER COLUMN "state" DROP DEFAULT;
ALTER TABLE "agents" ALTER COLUMN "state" TYPE "agent_state" USING "state"::"agent_state";
ALTER TABLE "agents" ALTER COLUMN "state" SET DEFAULT 'idle';
-- Set comment to column: "state" on table: "agents"
COMMENT ON COLUMN "agents"."state" IS 'What the agent is doing, which the reconcile loop writes from what it read off the pane. The three failure and setup values are the ones no frame read ever decides.';
