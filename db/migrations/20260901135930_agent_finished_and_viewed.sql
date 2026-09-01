-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "finished_at" timestamptz NULL, ADD COLUMN "viewed_at" timestamptz NULL;
