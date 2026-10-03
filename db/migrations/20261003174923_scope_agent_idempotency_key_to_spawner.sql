-- Drop index "agents_idempotency_key" from table: "agents"
DROP INDEX "agents_idempotency_key";
-- Create index "agents_idempotency_key" to table: "agents"
CREATE UNIQUE INDEX "agents_idempotency_key" ON "agents" ("engine_id", "spawned_by_agent_id", "idempotency_key") NULLS NOT DISTINCT WHERE (idempotency_key IS NOT NULL);
