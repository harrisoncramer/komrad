-- Drop index "agents_mark_active" from table: "agents"
DROP INDEX "agents_mark_active";
-- Create index "agents_mark_active" to table: "agents"
CREATE UNIQUE INDEX "agents_mark_active" ON "agents" ("mark") WHERE ((mark IS NOT NULL) AND (archived_at IS NULL));
