-- Add value to enum type: "agent_state"
ALTER TYPE "agent_state" ADD VALUE 'uncertain';
-- Set comment to column: "state" on table: "agents"
COMMENT ON COLUMN "agents"."state" IS 'What the agent is doing, which the reconcile loop writes from what it read off the pane. Uncertain is a pane weave captured but could not read. The three failure and setup values are the ones no frame read ever decides.';
