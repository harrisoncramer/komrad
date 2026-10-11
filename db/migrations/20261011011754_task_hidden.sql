-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "is_hidden" boolean NOT NULL DEFAULT false;
-- Set comment to column: "is_hidden" on table: "tasks"
COMMENT ON COLUMN "tasks"."is_hidden" IS 'Whether a person hid the task from the sidebar.';
