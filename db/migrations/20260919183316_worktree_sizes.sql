-- Create "worktree_sizes" table
CREATE TABLE "worktree_sizes" (
  "dir" text NOT NULL,
  "size_bytes" bigint NOT NULL,
  "counted_at" timestamptz NOT NULL,
  PRIMARY KEY ("dir")
);
-- Set comment to table: "worktree_sizes"
COMMENT ON TABLE "worktree_sizes" IS 'How many bytes each worktree directory held when the service last counted it. A row is dropped when its directory goes.';
-- Set comment to column: "dir" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."dir" IS 'The directory, absolute, as the worktree listing spells it.';
-- Set comment to column: "size_bytes" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."size_bytes" IS 'The sum of the sizes of every file under the directory, following no symlinks.';
-- Set comment to column: "counted_at" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."counted_at" IS 'When the count finished.';
