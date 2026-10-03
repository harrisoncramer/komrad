-- Drop index "agents_idempotency_key" from table: "agents"
DROP INDEX "agents_idempotency_key";
-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "requested_by_agent_id" uuid NULL;
-- Create index "agents_idempotency_key" to table: "agents"
CREATE UNIQUE INDEX "agents_idempotency_key" ON "agents" ("engine_id", "requested_by_agent_id", "parent_agent_id", "idempotency_key") NULLS NOT DISTINCT WHERE (idempotency_key IS NOT NULL);
