-- Add value to enum type: "agent_state"
ALTER TYPE "agent_state" ADD VALUE 'errored';
-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "turn_failed_at" timestamptz NULL, ADD COLUMN "turn_error" text NOT NULL DEFAULT '';
-- Set comment to column: "state" on table: "agents"
COMMENT ON COLUMN "agents"."state" IS 'What the agent is doing. Komrad captures the agent''s terminal every couple of seconds and writes here what it read there. Uncertain means the terminal was captured but could not be understood. Errored means the terminal is at rest after an API error ended the last turn. The setup and failure values never come from reading a terminal, so something else sets those.';
-- Set comment to column: "turn_failed_at" on table: "agents"
COMMENT ON COLUMN "agents"."turn_failed_at" IS 'When an API error last ended one of the agent''s turns. The last turn failed while this is no older than turn_ended_at.';
-- Set comment to column: "turn_error" on table: "agents"
COMMENT ON COLUMN "agents"."turn_error" IS 'What the agent''s CLI printed when an API error last ended one of its turns.';
