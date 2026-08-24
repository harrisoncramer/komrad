-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "state_seen_at" timestamptz NOT NULL DEFAULT now();
