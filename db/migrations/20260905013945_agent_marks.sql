-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN "tag", ADD COLUMN "mark" text NULL;
-- Create index "agents_mark_active" to table: "agents"
CREATE UNIQUE INDEX "agents_mark_active" ON "agents" ("mark") WHERE (mark IS NOT NULL);
