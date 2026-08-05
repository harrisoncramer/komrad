-- Modify "agent_messages" table
ALTER TABLE "agent_messages" ADD COLUMN "hops" integer NOT NULL DEFAULT 1;
