-- Modify "agent_messages" table
ALTER TABLE "agent_messages" ADD COLUMN "expires_at" timestamptz NULL;
UPDATE
    "agent_messages"
SET
    "expires_at" = "created_at" + INTERVAL '3 hours'
WHERE
    "expires_at" IS NULL;
ALTER TABLE "agent_messages" ALTER COLUMN "expires_at" SET NOT NULL;
-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "nudged_at" timestamptz NULL;
