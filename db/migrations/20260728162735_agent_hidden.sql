-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "is_hidden" boolean NOT NULL DEFAULT false;
