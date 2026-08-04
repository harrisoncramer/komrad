-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "state_changed_at" timestamptz NOT NULL DEFAULT now();
