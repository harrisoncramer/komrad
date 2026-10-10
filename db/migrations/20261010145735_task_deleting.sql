-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "deleting_at" timestamptz NULL, ADD COLUMN "cleaning_at" timestamptz NULL;
-- Set comment to column: "deleting_at" on table: "tasks"
COMMENT ON COLUMN "tasks"."deleting_at" IS 'When a delete claimed the task, which keeps it out of every listing until it is gone or the delete gives it back.';
-- Set comment to column: "cleaning_at" on table: "tasks"
COMMENT ON COLUMN "tasks"."cleaning_at" IS 'When the one cleanup that gives back a deleted task''s checkout and removes the row took it, so no other cleanup runs it as well.';
