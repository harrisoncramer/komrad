-- Modify "worktrees" table
ALTER TABLE "worktrees" ADD COLUMN "standing_read_at" timestamptz NULL, ADD COLUMN "dirty" boolean NOT NULL DEFAULT false, ADD COLUMN "sequencing" boolean NOT NULL DEFAULT false, ADD COLUMN "unlanded" integer NOT NULL DEFAULT 0, ADD COLUMN "standing_error" text NULL;
-- Set comment to column: "standing_read_at" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."standing_read_at" IS 'When git was last asked where the directory stands against its base, whether or not it answered, null until the first reading.';
-- Set comment to column: "dirty" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."dirty" IS 'Whether the directory held modified or untracked files at the last reading.';
-- Set comment to column: "sequencing" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."sequencing" IS 'Whether a merge, a rebase or a cherry-pick was part-way through at the last reading.';
-- Set comment to column: "unlanded" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."unlanded" IS 'How many of the branch''s own commits the base did not yet hold at the last reading.';
-- Set comment to column: "standing_error" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."standing_error" IS 'Why the last reading failed, null when it did not.';
