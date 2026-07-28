-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN "seen_at", ADD COLUMN "marked_at" timestamptz NULL;
