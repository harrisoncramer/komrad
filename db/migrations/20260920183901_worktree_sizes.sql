-- Create "worktree_sizes" table
CREATE TABLE "worktree_sizes" (
  "dir" text NOT NULL,
  "size_bytes" bigint NOT NULL DEFAULT 0,
  "measured_at" timestamptz NOT NULL,
  "error" text NULL,
  PRIMARY KEY ("dir")
);
-- Set comment to table: "worktree_sizes"
COMMENT ON TABLE "worktree_sizes" IS 'The disk one worktree directory takes, as the size loop or get_worktree last measured it. Keyed by the directory rather than an agent, since a directory outlives its row and one nobody answers for is what the worktrees screen is for.';
-- Set comment to column: "size_bytes" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."size_bytes" IS 'Blocks taken rather than length, a hard linked file counted once. Zero while error is set.';
-- Set comment to column: "measured_at" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."measured_at" IS 'When the walk last finished, whether or not it succeeded, so a failed directory is not walked again on every pass.';
-- Set comment to column: "error" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."error" IS 'Why the last walk failed, null when it did not.';
