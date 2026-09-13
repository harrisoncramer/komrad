-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "held_at" timestamptz NULL, ADD COLUMN "held_mid_turn" boolean NOT NULL DEFAULT false;
-- Set comment to column: "held_at" on table: "agents"
COMMENT ON COLUMN "agents"."held_at" IS 'When a person stopped this agent and took the tree, null while nobody is holding it. Held is not a state, since the states are decided by reading the pane and would overwrite it.';
-- Set comment to column: "held_mid_turn" on table: "agents"
COMMENT ON COLUMN "agents"."held_mid_turn" IS 'Whether the hold cut a turn that was still running, which is what decides whether letting go has to type the agent back into motion or merely stop holding it.';
