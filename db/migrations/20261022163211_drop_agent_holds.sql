-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN "held_at", DROP COLUMN "held_mid_turn", DROP COLUMN "nudge_sent_at", DROP COLUMN "held_by_agent_id";
-- Set comment to column: "last_rebase" on table: "tasks"
COMMENT ON COLUMN "tasks"."last_rebase" IS 'What the latest rebase of the task''s checkout came to; null until its first rebase.';
