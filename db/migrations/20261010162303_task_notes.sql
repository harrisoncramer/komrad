-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "notes" text NOT NULL DEFAULT '', ADD COLUMN "notes_updated_at" timestamptz NULL;
-- Set comment to column: "notes" on table: "tasks"
COMMENT ON COLUMN "tasks"."notes" IS 'What the agents on the task and the person have written down about it, as markdown.';
-- Set comment to column: "notes_updated_at" on table: "tasks"
COMMENT ON COLUMN "tasks"."notes_updated_at" IS 'When the notes were last written, null until they first are. A write names the value it read and is refused once it has moved.';
