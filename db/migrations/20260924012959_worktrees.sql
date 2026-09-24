-- Rename "worktree_sizes" table to "worktrees"
ALTER TABLE "worktree_sizes" RENAME TO "worktrees";
ALTER TABLE "worktrees" RENAME CONSTRAINT "worktree_sizes_pkey" TO "worktrees_pkey";
ALTER TABLE "worktrees" RENAME CONSTRAINT "worktree_sizes_tenant_id_fkey" TO "worktrees_tenant_id_fkey";
ALTER TABLE "worktrees" RENAME COLUMN "error" TO "size_error";
-- Modify "worktrees" table
ALTER TABLE "worktrees" ALTER COLUMN "measured_at" DROP NOT NULL, ADD COLUMN "repo_dir" text NOT NULL DEFAULT '', ADD COLUMN "branch" text NOT NULL DEFAULT '', ADD COLUMN "readable" boolean NOT NULL DEFAULT false, ADD COLUMN "mtime" timestamptz NULL;
-- Set comment to table: "worktrees"
COMMENT ON TABLE "worktrees" IS 'One directory under the worktree directories as the filesystem holds it, written by the size loop. Keyed by the directory rather than an agent, since a directory outlives its row and one nobody answers for is what the worktrees screen is for. A directory a delete has claimed has no row.';
-- Set comment to column: "repo_dir" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."repo_dir" IS 'The repository the directory was cut from, read off its .git file, empty when there is no such file.';
-- Set comment to column: "branch" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."branch" IS 'What the gitdir''s HEAD names, empty when it is detached or cannot be read.';
-- Set comment to column: "readable" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."readable" IS 'Whether the gitdir the .git file names is still there.';
-- Set comment to column: "mtime" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."mtime" IS 'When git last wrote the directory''s .git file, else the directory''s own modification time.';
-- Set comment to column: "size_bytes" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."size_bytes" IS 'Blocks taken rather than length, a hard linked file counted once. Zero until measured and while size_error is set.';
-- Set comment to column: "measured_at" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."measured_at" IS 'When the walk last finished, whether or not it succeeded, null until the first walk.';
-- Set comment to column: "size_error" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."size_error" IS 'Why the last walk failed, null when it did not.';
-- Set comment to column: "tenant_id" on table: "worktrees"
COMMENT ON COLUMN "worktrees"."tenant_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
