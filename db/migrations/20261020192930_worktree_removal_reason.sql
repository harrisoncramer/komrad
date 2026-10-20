-- Create enum type "worktree_removal_reason"
CREATE TYPE "worktree_removal_reason" AS ENUM ('deleted', 'gone');
-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "worktree_removal_reason" "worktree_removal_reason" NULL;
-- Set comment to column: "worktree_removal_reason" on table: "tasks"
COMMENT ON COLUMN "tasks"."worktree_removal_reason" IS 'Why the task''s worktree is no longer on disk: deleted when a delete removed it, gone when komrad found the directory gone without one; null while it is on disk or the task has none.';
