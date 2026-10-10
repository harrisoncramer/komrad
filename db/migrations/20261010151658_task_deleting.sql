-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "deleting_at" timestamptz NULL, ADD COLUMN "cleaning_at" timestamptz NULL, ADD COLUMN "failure_reason" text NOT NULL DEFAULT '';
-- Set comment to column: "deleting_at" on table: "tasks"
COMMENT ON COLUMN "tasks"."deleting_at" IS 'When a delete claimed the task, which keeps it out of every listing until it is gone or the delete gives it back.';
-- Set comment to column: "cleaning_at" on table: "tasks"
COMMENT ON COLUMN "tasks"."cleaning_at" IS 'When the one cleanup that gives back a deleted task''s checkout and removes the row took it, so no other cleanup runs it as well.';
-- Set comment to column: "failure_reason" on table: "tasks"
COMMENT ON COLUMN "tasks"."failure_reason" IS 'Why the task''s last delete could not give its checkout back, empty unless one failed.';
