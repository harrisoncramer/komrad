-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "archive_batch_id" uuid NULL;
-- Set comment to column: "archive_batch_id" on table: "agents"
COMMENT ON COLUMN "agents"."archive_batch_id" IS 'The archive this agent was taken out of live listings by, shared by every agent that archive took.';
