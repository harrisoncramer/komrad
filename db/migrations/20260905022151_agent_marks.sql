-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN IF EXISTS "tag", ADD COLUMN IF NOT EXISTS "mark" text NULL;
-- Create index "agents_mark_active" to table: "agents"
DROP INDEX IF EXISTS "agents_mark_active";
CREATE UNIQUE INDEX "agents_mark_active" ON "agents" ("mark") WHERE ((mark IS NOT NULL) AND (archived_at IS NULL));
