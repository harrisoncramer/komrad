-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN "marked_at", ADD COLUMN "mark" text NULL;
