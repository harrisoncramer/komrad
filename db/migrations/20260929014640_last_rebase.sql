-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "last_rebase" jsonb NULL;
-- Set comment to column: "last_rebase" on table: "agents"
COMMENT ON COLUMN "agents"."last_rebase" IS 'What the latest rebase of the agent''s checkout came to, as the result or the failure; null until its first rebase.';
