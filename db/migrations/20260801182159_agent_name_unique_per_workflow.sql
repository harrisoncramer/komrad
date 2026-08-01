-- Drop index "agents_name_active" from table: "agents"
DROP INDEX "agents_name_active";
-- Create index "agents_name_active" to table: "agents"
CREATE UNIQUE INDEX "agents_name_active" ON "agents" ("workflow_id", "name") WHERE (archived_at IS NULL);
