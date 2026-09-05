-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "reported_reason" text NOT NULL DEFAULT '';
-- Set comment to column: "reported_reason" on table: "agents"
COMMENT ON COLUMN "agents"."reported_reason" IS 'What the agent''s own CLI last reported it was doing, which annotates the state the pane decided rather than deciding one of its own. Cleared with the state it annotates whenever that changes, and by a turn ending, so empty means there is nothing to say.';
