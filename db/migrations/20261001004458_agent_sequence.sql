-- Create enum type "agent_sequence"
CREATE TYPE "agent_sequence" AS ENUM ('none', 'rebase', 'am', 'merge', 'cherry_pick', 'revert');
-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN "sequence_in_progress", ADD COLUMN "sequence" "agent_sequence" NOT NULL DEFAULT 'none';
-- Set comment to column: "sequence" on table: "agents"
COMMENT ON COLUMN "agents"."sequence" IS 'What git has part-way through the agent''s checkout and waiting on somebody: a rebase, an am, a merge, a cherry-pick or a revert, or none. Komrad reads the checkout every few seconds and writes what it found here, so settling the sequence by hand clears this on its own.';
