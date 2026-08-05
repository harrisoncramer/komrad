-- Modify "agent_messages" table
ALTER TABLE "agent_messages" ADD COLUMN "reported_at" timestamptz NULL;
