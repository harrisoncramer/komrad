-- Modify "pull_requests" table
ALTER TABLE "pull_requests" DROP COLUMN "stack_number", DROP COLUMN "stack_position", DROP COLUMN "stack_size", DROP COLUMN "stacking_refused_at", DROP COLUMN "rebase_awaited_since", DROP COLUMN "rebase_awaited_from", DROP COLUMN "retarget_onto";
