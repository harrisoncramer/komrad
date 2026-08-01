-- Drop index "agents_name_active" from table: "agents"
DROP INDEX "agents_name_active";
-- Rename rather than the drop and add the diff produces, which would throw away
-- when every archived agent went away and so empty the archive
ALTER TABLE "agents" RENAME COLUMN "deleted_at" TO "archived_at";
-- Create index "agents_name_active" to table: "agents"
CREATE UNIQUE INDEX "agents_name_active" ON "agents" ("name") WHERE (archived_at IS NULL);
