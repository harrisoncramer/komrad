-- Modify "pull_requests" table
ALTER TABLE "pull_requests" DROP COLUMN "stack_number", DROP COLUMN "stack_position", DROP COLUMN "stack_size", DROP COLUMN "stacking_refused_at", DROP COLUMN "rebase_awaited_since", DROP COLUMN "rebase_awaited_from", DROP COLUMN "retarget_onto", ADD COLUMN "retarget_refused_at" timestamptz NULL;
-- Set comment to column: "retarget_refused_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."retarget_refused_at" IS 'The changed_at the pull request had when the forge last refused to point it at the base branch, which komrad asks again only once changed_at moves past; null while nothing was refused.';
-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "left_parent_branch" text NULL;
-- Set comment to column: "left_parent_branch" on table: "tasks"
COMMENT ON COLUMN "tasks"."left_parent_branch" IS 'The branch of the parent this task left, which komrad moves the task''s pull request off onto the workflow''s base branch; null once nothing waits to move.';
