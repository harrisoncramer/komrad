-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "sequence_in_progress" boolean NOT NULL DEFAULT false;
-- Set comment to column: "sequence_in_progress" on table: "agents"
COMMENT ON COLUMN "agents"."sequence_in_progress" IS 'Whether a merge, a rebase or a cherry-pick is part-way through the agent''s checkout and waiting on somebody. Weave reads the checkout every few seconds and writes what it found here, so settling the sequence by hand clears this on its own.';
